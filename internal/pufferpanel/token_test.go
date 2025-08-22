package pufferpanel

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	dbpkg "modsentinel/internal/db"
	"modsentinel/internal/secrets"

	_ "modernc.org/sqlite"
)

func setup(t *testing.T) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := dbpkg.Init(db); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Setenv("SECRET_KEYSET", `{"primary":"1","keys":{"1":"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"}}`)
	km, err := secrets.Load(context.Background())
	if err != nil {
		t.Fatalf("load manager: %v", err)
	}
	Init(secrets.NewService(db, km))
}

func TestAddAuthCachesAndRefreshesToken(t *testing.T) {
	setup(t)
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

	if err := Set(Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	ctx := context.Background()

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
func TestClearRevokesCachedToken(t *testing.T) {
	setup(t)
	var tokenCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			tokenCalls++
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"access_token":"tok1","expires_in":3600}`)
			return
		}
		if r.URL.Path == "/data" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	if err := Set(Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/data", nil)
	if err := AddAuth(ctx, req); err != nil {
		t.Fatalf("AddAuth: %v", err)
	}
	if _, err := http.DefaultClient.Do(req); err != nil {
		t.Fatalf("request: %v", err)
	}
	if tokenCalls != 1 {
		t.Fatalf("token calls = %d, want 1", tokenCalls)
	}
	// clear credentials should drop cache
	if err := Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/data", nil)
	if err := AddAuth(ctx, req2); err == nil {
		t.Fatalf("expected error after clear")
	}
	if tokenCalls != 1 {
		t.Fatalf("token endpoint called after clear")
	}
}

func TestFetchTokenRedirectSameOrigin(t *testing.T) {
	setup(t)
	var tokenCalls atomic.Int64
	var redirectHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			http.Redirect(w, r, "https://evil.com/oauth2/token2", http.StatusFound)
		case "/oauth2/token2":
			tokenCalls.Add(1)
			redirectHost = r.Host
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	tok, _, err := fetchToken(context.Background(), Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"})
	if err != nil {
		t.Fatalf("fetchToken: %v", err)
	}
	base, _ := url.Parse(srv.URL)
	if tok != "tok" || tokenCalls.Load() != 1 || redirectHost != base.Host {
		t.Fatalf("tok=%q calls=%d host=%s want host %s", tok, tokenCalls.Load(), redirectHost, base.Host)
	}
}

func TestFetchTokenBypassesProxy(t *testing.T) {
	setup(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	tok, _, err := fetchToken(context.Background(), Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"})
	if err != nil {
		t.Fatalf("fetchToken: %v", err)
	}
	if tok != "tok" {
		t.Fatalf("tok=%q, want tok", tok)
	}
}
