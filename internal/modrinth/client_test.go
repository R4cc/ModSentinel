package modrinth

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	dbpkg "modsentinel/internal/db"
	logx "modsentinel/internal/logx"
	"modsentinel/internal/secrets"
	tokenpkg "modsentinel/internal/token"

	_ "modernc.org/sqlite"
)

// Test that NewClient configures transport timeouts and connection pooling.
func TestNewClientTransportConfig(t *testing.T) {
	c := NewClient()
	if c.http.Timeout != 30*time.Second {
		t.Fatalf("Timeout = %v, want 30s", c.http.Timeout)
	}
	tr, ok := c.http.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T, want *http.Transport", c.http.Transport)
	}
	if tr.TLSHandshakeTimeout != 5*time.Second {
		t.Fatalf("TLSHandshakeTimeout = %v, want 5s", tr.TLSHandshakeTimeout)
	}
	if tr.ResponseHeaderTimeout != 10*time.Second {
		t.Fatalf("ResponseHeaderTimeout = %v, want 10s", tr.ResponseHeaderTimeout)
	}
	if tr.ExpectContinueTimeout != 1*time.Second {
		t.Fatalf("ExpectContinueTimeout = %v, want 1s", tr.ExpectContinueTimeout)
	}
	if tr.MaxIdleConns != 100 {
		t.Fatalf("MaxIdleConns = %d, want 100", tr.MaxIdleConns)
	}
	if tr.MaxIdleConnsPerHost != 10 {
		t.Fatalf("MaxIdleConnsPerHost = %d, want 10", tr.MaxIdleConnsPerHost)
	}
	if tr.MaxConnsPerHost != 10 {
		t.Fatalf("MaxConnsPerHost = %d, want 10", tr.MaxConnsPerHost)
	}
	if tr.IdleConnTimeout != 90*time.Second {
		t.Fatalf("IdleConnTimeout = %v, want 90s", tr.IdleConnTimeout)
	}
}

// Test that the client attaches the Authorization header when a token exists.
func TestClientAddsAuthorizationHeader(t *testing.T) {
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := dbpkg.Init(db); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := dbpkg.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	tokenpkg.Init(secrets.NewService(db))
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
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := dbpkg.Init(db); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := dbpkg.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	tokenpkg.Init(secrets.NewService(db))
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

// Test that the client sends the expected User-Agent.
func TestClientSetsUserAgent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != userAgent {
			t.Fatalf("User-Agent = %q want %q", got, userAgent)
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

// Test that request metadata is logged without leaking tokens.
func TestClientLogsRequest(t *testing.T) {
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := dbpkg.Init(db); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := dbpkg.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	tokenpkg.Init(secrets.NewService(db))
	const tok = "abcd1234"
	if err := tokenpkg.SetToken(tok); err != nil {
		t.Fatalf("set token: %v", err)
	}
	var buf bytes.Buffer
	log.Logger = zerolog.New(logx.NewRedactor(&buf)).With().Timestamp().Logger()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer ts.Close()

	c := &Client{http: ts.Client()}
	req, err := http.NewRequest(http.MethodGet, ts.URL+"?token=secret&key=apikey", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := c.do(req, &struct{}{}); err != nil {
		t.Fatalf("do: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "\"event\":\"modrinth_request\"") {
		t.Fatalf("expected modrinth_request event, got %s", out)
	}
	if !strings.Contains(out, "\"method\":\"GET\"") {
		t.Fatalf("expected method GET, got %s", out)
	}
	if !strings.Contains(out, "\"status\":\"200\"") {
		t.Fatalf("expected status 200, got %s", out)
	}
	if !strings.Contains(out, "\"attempt\":\"1\"") {
		t.Fatalf("expected attempt 1, got %s", out)
	}
	if strings.Contains(out, "secret") || strings.Contains(out, tok) {
		t.Fatalf("log leaked token: %s", out)
	}
	if !strings.Contains(out, "token=REDACTED") || !strings.Contains(out, "key=REDACTED") {
		t.Fatalf("expected redacted URL, got %s", out)
	}
}

// Test that the client emits a success metric.
func TestClientEmitsSuccessMetric(t *testing.T) {
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := dbpkg.Init(db); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := dbpkg.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	tokenpkg.Init(secrets.NewService(db))
	var buf bytes.Buffer
	log.Logger = zerolog.New(logx.NewRedactor(&buf)).With().Timestamp().Logger()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	out := buf.String()
	if !strings.Contains(out, "\"event\":\"modrinth_result\"") || !strings.Contains(out, "\"outcome\":\"success\"") {
		t.Fatalf("expected success metric, got %s", out)
	}
}

// Test that the client emits an error metric with kind classification.
func TestClientEmitsErrorMetric(t *testing.T) {
	oldRand := randDuration
	randDuration = func(d time.Duration) time.Duration { return 0 }
	defer func() { randDuration = oldRand }()
	oldSleep := sleep
	sleep = func(time.Duration) {}
	defer func() { sleep = oldSleep }()

	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := dbpkg.Init(db); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := dbpkg.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	tokenpkg.Init(secrets.NewService(db))
	var buf bytes.Buffer
	log.Logger = zerolog.New(logx.NewRedactor(&buf)).With().Timestamp().Logger()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	c := &Client{http: ts.Client()}
	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := c.do(req, &struct{}{}); err == nil {
		t.Fatalf("expected error")
	}
	out := buf.String()
	if !strings.Contains(out, "\"event\":\"modrinth_result\"") || !strings.Contains(out, "\"outcome\":\"error\"") || !strings.Contains(out, "\"kind\":\"server_error\"") {
		t.Fatalf("expected error metric, got %s", out)
	}
}

// Test that the client retries with exponential backoff on server errors.
func TestClientBackoff(t *testing.T) {
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := dbpkg.Init(db); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := dbpkg.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	tokenpkg.Init(secrets.NewService(db))
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
	oldRand := randDuration
	randDuration = func(time.Duration) time.Duration { return 0 }
	defer func() { randDuration = oldRand }()
	var sleeps []time.Duration
	oldSleep := sleep
	sleep = func(d time.Duration) { sleeps = append(sleeps, d) }
	defer func() { sleep = oldSleep }()

	if err := c.do(req, &struct{}{}); err != nil {
		t.Fatalf("do: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
	want := []time.Duration{250 * time.Millisecond, 500 * time.Millisecond}
	if !reflect.DeepEqual(sleeps, want) {
		t.Fatalf("sleeps = %v want %v", sleeps, want)
	}
}

// Test that the client retries on 429 responses with backoff and jitter.
func TestClientBackoffTooManyRequests(t *testing.T) {
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := dbpkg.Init(db); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := dbpkg.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	tokenpkg.Init(secrets.NewService(db))
	oldRand := randDuration
	randDuration = func(time.Duration) time.Duration { return 0 }
	defer func() { randDuration = oldRand }()
	var sleeps []time.Duration
	oldSleep := sleep
	sleep = func(d time.Duration) { sleeps = append(sleeps, d) }
	defer func() { sleep = oldSleep }()
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
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
	if err := c.do(req, &struct{}{}); err != nil {
		t.Fatalf("do: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
	want := []time.Duration{250 * time.Millisecond, 500 * time.Millisecond}
	if !reflect.DeepEqual(sleeps, want) {
		t.Fatalf("sleeps = %v want %v", sleeps, want)
	}
}

// Test that the client respects Retry-After headers for rate limiting.
func TestClientRetryAfterHeader(t *testing.T) {
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := dbpkg.Init(db); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := dbpkg.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	tokenpkg.Init(secrets.NewService(db))
	oldRand := randDuration
	randDuration = func(d time.Duration) time.Duration { return 0 }
	defer func() { randDuration = oldRand }()
	var slept time.Duration
	oldSleep := sleep
	sleep = func(d time.Duration) { slept += d }
	defer func() { sleep = oldSleep }()
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
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
	if err := c.do(req, &struct{}{}); err != nil {
		t.Fatalf("do: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if slept < 2*time.Second {
		t.Fatalf("expected sleep at least 2s, got %v", slept)
	}
}

// Test that repeated 429 responses escalate global backoff.
func TestClientRateLimitEscalation(t *testing.T) {
	oldRand := randDuration
	randDuration = func(d time.Duration) time.Duration { return 0 }
	defer func() { randDuration = oldRand }()
	var sleeps []time.Duration
	oldSleep := sleep
	sleep = func(d time.Duration) { sleeps = append(sleeps, d) }
	defer func() { sleep = oldSleep }()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer ts.Close()
	c := &Client{http: ts.Client()}
	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := c.do(req, &struct{}{}); err == nil {
		t.Fatalf("expected error")
	}
	if c.backoff != time.Second {
		t.Fatalf("expected backoff 1s, got %v", c.backoff)
	}
	sleeps = nil
	req2, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := c.do(req2, &struct{}{}); err == nil {
		t.Fatalf("expected error")
	}
	if len(sleeps) == 0 || sleeps[0] != time.Second {
		t.Fatalf("expected initial sleep 1s, got %v", sleeps)
	}
	if c.backoff != 2*time.Second {
		t.Fatalf("expected backoff 2s, got %v", c.backoff)
	}
}

// Test that 401 responses are surfaced as an Error with the correct status.
func TestClientInvalidToken(t *testing.T) {
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := dbpkg.Init(db); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := dbpkg.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	tokenpkg.Init(secrets.NewService(db))
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
		if me.Kind != KindClient {
			t.Fatalf("kind = %v want %v", me.Kind, KindClient)
		}
	}
}

func TestClientErrorClassificationHTTP(t *testing.T) {
	oldRand := randDuration
	randDuration = func(d time.Duration) time.Duration { return 0 }
	defer func() { randDuration = oldRand }()
	oldSleep := sleep
	sleep = func(time.Duration) {}
	defer func() { sleep = oldSleep }()
	cases := []struct {
		status int
		kind   Kind
	}{
		{http.StatusTooManyRequests, KindRateLimited},
		{http.StatusInternalServerError, KindServer},
		{http.StatusNotFound, KindClient},
	}
	for _, tt := range cases {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tt.status)
			w.Write([]byte(`{"error":"msg"}`))
		}))
		c := &Client{http: ts.Client()}
		req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		if err := c.do(req, &struct{}{}); err == nil {
			t.Fatalf("expected error for status %d", tt.status)
		} else if me, ok := err.(*Error); !ok {
			t.Fatalf("unexpected error type: %T", err)
		} else {
			if me.Kind != tt.kind {
				t.Fatalf("kind = %v want %v", me.Kind, tt.kind)
			}
			if me.Status != tt.status {
				t.Fatalf("status = %d want %d", me.Status, tt.status)
			}
		}
		ts.Close()
	}
}

func TestClientErrorKindTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer ts.Close()
	c := &Client{http: &http.Client{Timeout: 100 * time.Millisecond}}
	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := c.do(req, &struct{}{}); err == nil {
		t.Fatalf("expected timeout error")
	} else if me, ok := err.(*Error); !ok {
		t.Fatalf("unexpected error type: %T", err)
	} else if me.Kind != KindTimeout {
		t.Fatalf("kind = %v want %v", me.Kind, KindTimeout)
	}
}

func TestClientErrorKindCanceled(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer ts.Close()
	c := &Client{http: ts.Client()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := c.do(req, &struct{}{}); err == nil {
		t.Fatalf("expected canceled error")
	} else if me, ok := err.(*Error); !ok {
		t.Fatalf("unexpected error type: %T", err)
	} else if me.Kind != KindCanceled {
		t.Fatalf("kind = %v want %v", me.Kind, KindCanceled)
	}
}

func TestNormalizeQuery(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  FancyMod-1.2.3  ", "fancymod"},
		{"Example_MOD-2.0", "example_mod"},
		{"some_mod_1.18", "some_mod"},
		{"Sodium", "sodium"},
	}
	for _, tt := range cases {
		if got := normalizeQuery(tt.in); got != tt.want {
			t.Errorf("normalizeQuery(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}

func TestResolveProjectDirect(t *testing.T) {
	paths := []string{}
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		if req.URL.Path != "/v2/project/sodium" {
			t.Fatalf("unexpected path %s", req.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"title":"Sodium","icon_url":""}`)),
			Header:     make(http.Header),
		}, nil
	})
	c := &Client{http: &http.Client{Transport: rt}}
	proj, slug, err := c.Resolve(context.Background(), "sodium")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if slug != "sodium" {
		t.Fatalf("slug = %q; want %q", slug, "sodium")
	}
	if proj.Title != "Sodium" {
		t.Fatalf("title = %q; want %q", proj.Title, "Sodium")
	}
	if len(paths) != 1 || paths[0] != "/v2/project/sodium" {
		t.Fatalf("paths = %v; want [/v2/project/sodium]", paths)
	}
}

func TestResolveProjectSearchFallback(t *testing.T) {
	paths := []string{}
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		switch req.URL.Path {
		case "/v2/project/foo":
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader(`{"error":"not found"}`)),
				Header:     make(http.Header),
			}, nil
		case "/v2/search":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"hits":[{"project_id":"1","slug":"sodium","title":"Sodium"}]}`)),
				Header:     make(http.Header),
			}, nil
		case "/v2/project/sodium":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"title":"Sodium","icon_url":""}`)),
				Header:     make(http.Header),
			}, nil
		default:
			t.Fatalf("unexpected path %s", req.URL.Path)
		}
		return nil, nil
	})
	c := &Client{http: &http.Client{Transport: rt}}
	proj, slug, err := c.Resolve(context.Background(), "foo")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if slug != "sodium" {
		t.Fatalf("slug = %q; want %q", slug, "sodium")
	}
	if proj.Title != "Sodium" {
		t.Fatalf("title = %q; want %q", proj.Title, "Sodium")
	}
	want := []string{"/v2/project/foo", "/v2/search", "/v2/project/sodium"}
	if len(paths) != len(want) {
		t.Fatalf("paths = %v; want %v", paths, want)
	}
	for i, p := range want {
		if paths[i] != p {
			t.Fatalf("paths[%d] = %s; want %s", i, paths[i], p)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestSearchNormalizesQuery(t *testing.T) {
	var got string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("query")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"hits":[]}`))
	}))
	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = u.Scheme
		req.URL.Host = u.Host
		return http.DefaultTransport.RoundTrip(req)
	})
	c := &Client{http: &http.Client{Transport: rt}}
	if _, err := c.Search(context.Background(), " MyMod-1.2.3 "); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got != "mymod" {
		t.Fatalf("query = %q, want %q", got, "mymod")
	}
}

func TestSearchRejectsInvalidQuery(t *testing.T) {
	c := NewClient()
	if _, err := c.Search(context.Background(), "   "); err == nil {
		t.Fatal("expected error for empty query")
	}
	bad := "mo" + string(rune(0x85)) + "d"
	if _, err := c.Search(context.Background(), bad); err == nil {
		t.Fatal("expected error for control character")
	}
}

// Test that concurrent identical requests share a single underlying call.
func TestClientSingleFlightDedupe(t *testing.T) {
	var requests int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer ts.Close()

	c := &Client{http: ts.Client()}
	const goroutines = 5
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			<-start
			req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
			if err != nil {
				t.Errorf("new request: %v", err)
				return
			}
			if err := c.do(req, &struct{}{}); err != nil {
				t.Errorf("do: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("expected 1 request, got %d", got)
	}
}

// Test that successful responses are cached for the TTL duration.
func TestClientCachesResponses(t *testing.T) {
	var requests int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer ts.Close()

	c := &Client{http: ts.Client(), ttl: time.Minute}

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := c.do(req, &struct{}{}); err != nil {
		t.Fatalf("do 1: %v", err)
	}

	req2, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := c.do(req2, &struct{}{}); err != nil {
		t.Fatalf("do 2: %v", err)
	}

	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("expected 1 request, got %d", got)
	}
}

// Test that cached entries expire after the TTL.
func TestClientCacheTTL(t *testing.T) {
	var requests int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer ts.Close()

	c := &Client{http: ts.Client(), ttl: 10 * time.Millisecond}

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := c.do(req, &struct{}{}); err != nil {
		t.Fatalf("do 1: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	req2, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := c.do(req2, &struct{}{}); err != nil {
		t.Fatalf("do 2: %v", err)
	}

	if got := atomic.LoadInt32(&requests); got != 2 {
		t.Fatalf("expected 2 requests, got %d", got)
	}
}

// Test that error responses are not cached.
func TestClientDoesNotCacheErrors(t *testing.T) {
	var requests int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&requests, 1)
		if n <= 3 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"fail"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer ts.Close()

	c := &Client{http: ts.Client(), ttl: time.Minute}

	req1, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := c.do(req1, &struct{}{}); err == nil {
		t.Fatalf("expected error")
	}

	req2, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := c.do(req2, &struct{}{}); err != nil {
		t.Fatalf("second request: %v", err)
	}

	if got := atomic.LoadInt32(&requests); got != 4 {
		t.Fatalf("expected 4 requests, got %d", got)
	}
}

// Test that Search retries on server errors and eventually succeeds.
func TestSearchRecoversFromServerError(t *testing.T) {
	var attempts int32
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			resp := &http.Response{
				StatusCode: http.StatusInternalServerError,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("{}")),
			}
			return resp, nil
		}
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"hits":[{"slug":"ok"}]}`)),
		}
		return resp, nil
	})
	c := NewClient()
	oldRand := randDuration
	randDuration = func(time.Duration) time.Duration { return 0 }
	defer func() { randDuration = oldRand }()
	oldSleep := sleep
	sleep = func(time.Duration) {}
	defer func() { sleep = oldSleep }()
	c.http = &http.Client{Transport: rt}
	res, err := c.Search(context.Background(), "ok")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if len(res.Hits) != 1 || res.Hits[0].Slug != "ok" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

// Test that Search retries on rate limits and succeeds.
func TestSearchRecoversFromRateLimit(t *testing.T) {
	var attempts int32
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			resp := &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
			}
			return resp, nil
		}
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"hits":[{"slug":"ok"}]}`)),
		}
		return resp, nil
	})
	c := NewClient()
	oldRand := randDuration
	randDuration = func(time.Duration) time.Duration { return 0 }
	defer func() { randDuration = oldRand }()
	oldSleep := sleep
	sleep = func(time.Duration) {}
	defer func() { sleep = oldSleep }()
	c.http = &http.Client{Transport: rt}
	res, err := c.Search(context.Background(), "ok")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if len(res.Hits) != 1 || res.Hits[0].Slug != "ok" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

// Test that a timeout error does not prevent subsequent requests from succeeding.
func TestSearchRecoversAfterTimeout(t *testing.T) {
	var attempts int32
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			return nil, context.DeadlineExceeded
		}
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"hits":[{"slug":"ok"}]}`)),
		}
		return resp, nil
	})
	c := NewClient()
	c.http = &http.Client{Transport: rt}
	if _, err := c.Search(context.Background(), "ok"); err == nil {
		t.Fatal("expected timeout error")
	}
	res, err := c.Search(context.Background(), "ok")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if len(res.Hits) != 1 || res.Hits[0].Slug != "ok" {
		t.Fatalf("unexpected result: %+v", res)
	}
}
