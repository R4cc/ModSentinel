package pufferpanel

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAddAuthCachesAndRefreshesToken(t *testing.T) {
	t.Parallel()
	var tokenCalls int
	var lastAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			tokenCalls++
			w.Header().Set("Content-Type", "application/json")
			if tokenCalls == 1 {
				fmt.Fprint(w, `{"access_token":"tok1","expires_in":3600}`)
			} else {
				fmt.Fprint(w, `{"access_token":"tok2","expires_in":3600}`)
			}
		case "/data":
			lastAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	os.Setenv("MODSENTINEL_PUFFERPANEL_PATH", filepath.Join(dir, "creds"))
	t.Cleanup(func() { os.Unsetenv("MODSENTINEL_PUFFERPANEL_PATH") })
	if err := Set(Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	ctx := context.Background()

	// first request, fetch token
	req1, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/data", nil)
	if err := AddAuth(ctx, req1); err != nil {
		t.Fatalf("AddAuth 1: %v", err)
	}
	if _, err := http.DefaultClient.Do(req1); err != nil {
		t.Fatalf("request 1: %v", err)
	}
	if tokenCalls != 1 {
		t.Fatalf("token calls = %d, want 1", tokenCalls)
	}
	if lastAuth != "Bearer tok1" {
		t.Fatalf("auth header = %s, want Bearer tok1", lastAuth)
	}

	// second request before expiry, should reuse token
	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/data", nil)
	if err := AddAuth(ctx, req2); err != nil {
		t.Fatalf("AddAuth 2: %v", err)
	}
	if _, err := http.DefaultClient.Do(req2); err != nil {
		t.Fatalf("request 2: %v", err)
	}
	if tokenCalls != 1 {
		t.Fatalf("token calls = %d, want 1", tokenCalls)
	}

	// force expiration and ensure refresh
	tokenMu.Lock()
	tokenExpiry = time.Now().Add(-time.Second)
	tokenMu.Unlock()
	req3, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/data", nil)
	if err := AddAuth(ctx, req3); err != nil {
		t.Fatalf("AddAuth 3: %v", err)
	}
	if _, err := http.DefaultClient.Do(req3); err != nil {
		t.Fatalf("request 3: %v", err)
	}
	if tokenCalls != 2 {
		t.Fatalf("token calls = %d, want 2", tokenCalls)
	}
	if lastAuth != "Bearer tok2" {
		t.Fatalf("auth header = %s, want Bearer tok2", lastAuth)
	}
}
