package pufferpanel

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	logx "modsentinel/internal/logx"

	dbpkg "modsentinel/internal/db"
	"modsentinel/internal/oauth"
	"modsentinel/internal/secrets"
	"modsentinel/internal/settings"

	_ "modernc.org/sqlite"
)

func setupCreds(t *testing.T, base string) {
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := dbpkg.Init(db); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := dbpkg.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	t.Setenv("MODSENTINEL_NODE_KEY", nodeKey)
	km, err := secrets.Load(context.Background(), db)
	if err != nil {
		t.Fatalf("load keys: %v", err)
	}
	svc := secrets.NewService(db, km)
	cfg := settings.New(db)
	oauthSvc := oauth.New(db, km)
	Init(svc, cfg, oauthSvc)
	resetToken()
	serverCache = sync.Map{}
	if err := Set(Credentials{BaseURL: base, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
}

func TestListServers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case "/api/servers":
			switch r.URL.Query().Get("page") {
			case "", "1":
				fmt.Fprint(w, `{"paging":{"page":1,"size":1,"total":2,"next":"/api/servers?page=2"},"servers":[{"id":"1","name":"One"}]}`)
			case "2":
				fmt.Fprint(w, `{"paging":{"page":2,"size":1,"total":2,"next":""},"servers":[{"id":"2","name":"Two"}]}`)
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setupCreds(t, srv.URL)
	svs, err := ListServers(context.Background())
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	if len(svs) != 2 || svs[0].ID != "1" || svs[1].ID != "2" {
		t.Fatalf("unexpected servers %+v", svs)
	}
}

func TestListServersErrors(t *testing.T) {
	cases := []struct {
		status  int
		message string
	}{
		{http.StatusUnauthorized, "unauth"},
		{http.StatusForbidden, "nope"},
		{http.StatusInternalServerError, "broken"},
	}
	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/oauth2/token":
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
				case "/api/servers":
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tc.status)
					fmt.Fprintf(w, `{"code":%d,"message":"%s","requestId":"x"}`, tc.status, tc.message)
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			setupCreds(t, srv.URL)
			_, err := ListServers(context.Background())
			if tc.status == http.StatusForbidden {
				if !errors.Is(err, ErrForbidden) {
					t.Fatalf("err = %v, want ErrForbidden", err)
				}
			} else {
				if err == nil || err.Error() != tc.message {
					t.Fatalf("err = %v, want %q", err, tc.message)
				}
			}
		})
	}
}

func TestListServersLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case "/api/servers":
			page := 1
			if p := r.URL.Query().Get("page"); p != "" {
				page, _ = strconv.Atoi(p)
			}
			next := ""
			if page < 5 {
				next = fmt.Sprintf("/api/servers?page=%d", page+1)
			}
			fmt.Fprintf(w, `{"paging":{"page":%d,"size":1,"total":5,"next":"%s"},"servers":[{"id":"%d","name":"S%d"}]}`,
				page, next, page+1, page+1)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setupCreds(t, srv.URL)
	old := maxServers
	maxServers = 3
	t.Cleanup(func() { maxServers = old })
	svs, err := ListServers(context.Background())
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	if len(svs) != 3 {
		t.Fatalf("len = %d, want 3", len(svs))
	}
}

func TestListServersDebounce(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case "/api/servers":
			if p := r.URL.Query().Get("page"); p == "" || p == "1" {
				calls.Add(1)
				fmt.Fprint(w, `{"paging":{"page":1,"size":1,"total":1,"next":""},"servers":[{"id":"1","name":"One"}]}`)
			} else {
				fmt.Fprint(w, `{"paging":{"page":2,"size":1,"total":1,"next":""},"servers":[]}`)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setupCreds(t, srv.URL)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := ListServers(context.Background()); err != nil {
				t.Errorf("ListServers: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestListServersCache(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case "/api/servers":
			calls.Add(1)
			fmt.Fprint(w, `{"paging":{"page":1,"size":1,"total":1,"next":""},"servers":[{"id":"1","name":"One"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setupCreds(t, srv.URL)
	old := serverTTL
	serverTTL = 50 * time.Millisecond
	t.Cleanup(func() { serverTTL = old })
	ctx := context.Background()
	if _, err := ListServers(ctx); err != nil {
		t.Fatalf("ListServers 1: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls after first = %d, want 1", calls.Load())
	}
	if _, err := ListServers(ctx); err != nil {
		t.Fatalf("ListServers 2: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls after cache = %d, want 1", calls.Load())
	}
	time.Sleep(serverTTL + 20*time.Millisecond)
	if _, err := ListServers(ctx); err != nil {
		t.Fatalf("ListServers 3: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls after ttl = %d, want 2", calls.Load())
	}
}

func TestListServersConcurrentCache(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case "/api/servers":
			calls.Add(1)
			fmt.Fprint(w, `{"paging":{"page":1,"size":1,"total":1,"next":""},"servers":[{"id":"1","name":"One"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setupCreds(t, srv.URL)
	old := serverTTL
	serverTTL = 50 * time.Millisecond
	t.Cleanup(func() { serverTTL = old })

	ctx := context.Background()

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := ListServers(ctx); err != nil {
				t.Errorf("ListServers: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("calls after first = %d, want 1", calls.Load())
	}

	time.Sleep(serverTTL + 20*time.Millisecond)

	start = make(chan struct{})
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := ListServers(ctx); err != nil {
				t.Errorf("ListServers 2: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if calls.Load() != 2 {
		t.Fatalf("calls after second = %d, want 2", calls.Load())
	}
}

func TestListServersRefreshesTokenOnUnauthorized(t *testing.T) {
	var tokenCalls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			n := tokenCalls.Add(1)
			fmt.Fprintf(w, `{"access_token":"tok%d","expires_in":3600}`, n)
		case "/api/servers":
			if r.Header.Get("Authorization") == "Bearer tok1" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			fmt.Fprint(w, `{"paging":{"page":1,"size":1,"total":1,"next":""},"servers":[{"id":"1","name":"One"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setupCreds(t, srv.URL)
	ctx := context.Background()
	if _, err := ListServers(ctx); err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	if tokenCalls.Load() != 2 {
		t.Fatalf("token calls = %d, want 2", tokenCalls.Load())
	}
	if _, err := ListServers(ctx); err != nil {
		t.Fatalf("ListServers 2: %v", err)
	}
	if tokenCalls.Load() != 2 {
		t.Fatalf("token calls after second list = %d, want 2", tokenCalls.Load())
	}
}

func TestListServersNextAbsolute(t *testing.T) {
	var calls atomic.Int64
	var page2Host string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case "/api/servers":
			calls.Add(1)
			if r.URL.Query().Get("page") == "2" {
				page2Host = r.Host
				fmt.Fprint(w, `{"paging":{"page":2,"size":1,"total":2,"next":""},"servers":[{"id":"2","name":"Two"}]}`)
				return
			}
			fmt.Fprint(w, `{"paging":{"page":1,"size":1,"total":2,"next":"https://evil.com/api/servers?page=2"},"servers":[{"id":"1","name":"One"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setupCreds(t, srv.URL)
	svs, err := ListServers(context.Background())
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	base, _ := url.Parse(srv.URL)
	if len(svs) != 2 || calls.Load() != 2 || page2Host != base.Host {
		t.Fatalf("servers=%d calls=%d host=%s want host %s", len(svs), calls.Load(), page2Host, base.Host)
	}
}

func TestListServersRedirectSameOrigin(t *testing.T) {
	var calls atomic.Int64
	var redirectHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case "/api/servers":
			http.Redirect(w, r, "https://evil.com/api/servers2", http.StatusFound)
		case "/api/servers2":
			calls.Add(1)
			redirectHost = r.Host
			fmt.Fprint(w, `{"paging":{"page":1,"size":1,"total":1,"next":""},"servers":[{"id":"1","name":"One"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setupCreds(t, srv.URL)
	svs, err := ListServers(context.Background())
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	base, _ := url.Parse(srv.URL)
	if len(svs) != 1 || calls.Load() != 1 || redirectHost != base.Host {
		t.Fatalf("servers=%d calls=%d host=%s want host %s", len(svs), calls.Load(), redirectHost, base.Host)
	}
}

func TestListServersBypassesProxy(t *testing.T) {
	var proxyHits int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&proxyHits, 1)
		http.Error(w, "should not be used", http.StatusTeapot)
	}))
	defer proxy.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case "/api/servers":
			fmt.Fprint(w, `{"paging":{"page":1,"size":1,"total":1,"next":""},"servers":[{"id":"1","name":"One"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)

	setupCreds(t, srv.URL)
	svs, err := ListServers(context.Background())
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	if len(svs) != 1 {
		t.Fatalf("len=%d, want 1", len(svs))
	}
	if atomic.LoadInt32(&proxyHits) != 0 {
		t.Fatalf("proxy saw %d requests", proxyHits)
	}
}

func TestListServersTelemetry(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Logger
	log.Logger = zerolog.New(logx.NewRedactor(zerolog.SyncWriter(&buf))).With().Timestamp().Logger()
	t.Cleanup(func() { log.Logger = prev })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case "/api/servers":
			fmt.Fprint(w, `{"paging":{"page":1,"size":1,"total":1,"next":""},"servers":[{"id":"1","name":"One"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	setupCreds(t, srv.URL)
	old := serverTTL
	serverTTL = 50 * time.Millisecond
	t.Cleanup(func() { serverTTL = old })

	ctx := context.Background()

	if _, err := ListServers(ctx); err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "\"resource\":\"pufferpanel.servers\"") || !strings.Contains(out, "\"deduped\":\"false\"") ||
		!strings.Contains(out, "\"cache_hit\":\"false\"") || !strings.Contains(out, "\"status\":\"ok\"") ||
		!strings.Contains(out, "\"duration_ms\"") {
		t.Fatalf("missing fields: %s", out)
	}

	buf.Reset()
	serverCache = sync.Map{}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := ListServers(ctx); err != nil {
				t.Errorf("ListServers: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	out = buf.String()
	if !strings.Contains(out, "\"deduped\"") {
		t.Fatalf("expected deduped field: %s", out)
	}

	buf.Reset()
	if _, err := ListServers(ctx); err != nil {
		t.Fatalf("ListServers cache: %v", err)
	}
	out = buf.String()
	if !strings.Contains(out, "\"cache_hit\":\"true\"") {
		t.Fatalf("expected cache hit: %s", out)
	}
}
