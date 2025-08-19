package modrinth

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	tokenpkg "modsentinel/internal/token"
)

// Test that the client attaches the Authorization header when a token exists.
func TestClientAddsAuthorizationHeader(t *testing.T) {
	t.Setenv("MODSENTINEL_TOKEN_PATH", filepath.Join(t.TempDir(), "token"))
	const tok = "abcdef1234"
	if err := tokenpkg.SetToken(tok); err != nil {
		t.Fatalf("set token: %v", err)
	}
	var got string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer ts.Close()

	c := &Client{http: ts.Client()}
	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := c.do(req, &struct{}{}); err != nil {
		t.Fatalf("do: %v", err)
	}
	want := "Bearer " + tok
	if got != want {
		t.Fatalf("authorization header = %q want %q", got, want)
	}
}

// Test that the client does not send Authorization when no token is stored.
func TestClientOmitsAuthorizationHeader(t *testing.T) {
	t.Setenv("MODSENTINEL_TOKEN_PATH", filepath.Join(t.TempDir(), "token"))
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := r.Header.Get("Authorization"); h != "" {
			t.Fatalf("unexpected authorization header: %q", h)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer ts.Close()

	c := &Client{http: ts.Client()}
	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := c.do(req, &struct{}{}); err != nil {
		t.Fatalf("do: %v", err)
	}
}

// Test that the client retries with exponential backoff on server errors.
func TestClientBackoff(t *testing.T) {
	t.Setenv("MODSENTINEL_TOKEN_PATH", filepath.Join(t.TempDir(), "token"))
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer ts.Close()

	c := &Client{http: ts.Client()}
	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	start := time.Now()
	if err := c.do(req, &struct{}{}); err != nil {
		t.Fatalf("do: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
	if elapsed := time.Since(start); elapsed < 700*time.Millisecond {
		t.Fatalf("expected backoff delay, got %v", elapsed)
	}
}

// Test that 401 responses are surfaced as an Error with the correct status.
func TestClientInvalidToken(t *testing.T) {
	t.Setenv("MODSENTINEL_TOKEN_PATH", filepath.Join(t.TempDir(), "token"))
	const tok = "badtoken"
	if err := tokenpkg.SetToken(tok); err != nil {
		t.Fatalf("set token: %v", err)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer ts.Close()

	c := &Client{http: ts.Client()}
	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := c.do(req, &struct{}{}); err == nil {
		t.Fatalf("expected error")
	} else {
		me, ok := err.(*Error)
		if !ok {
			t.Fatalf("unexpected error type: %T", err)
		}
		if me.Status != http.StatusUnauthorized {
			t.Fatalf("status = %d want %d", me.Status, http.StatusUnauthorized)
		}
	}
}
