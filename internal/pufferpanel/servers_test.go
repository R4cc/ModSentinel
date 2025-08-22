package pufferpanel

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	dbpkg "modsentinel/internal/db"
	"modsentinel/internal/secrets"

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
	t.Setenv("SECRET_KEYSET", `{"primary":"1","keys":{"1":"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"}}`)
	km, err := secrets.Load(context.Background())
	if err != nil {
		t.Fatalf("load keys: %v", err)
	}
	Init(secrets.NewService(db, km))
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
			case "", "0":
				fmt.Fprint(w, `{"paging":{"page":0,"size":1,"total":2},"servers":[{"id":"1","name":"One"}]}`)
			case "1":
				fmt.Fprint(w, `{"paging":{"page":1,"size":1,"total":2},"servers":[{"id":"2","name":"Two"}]}`)
			default:
				fmt.Fprint(w, `{"paging":{"page":2,"size":1,"total":2},"servers":[]}`)
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
			if err == nil || err.Error() != tc.message {
				t.Fatalf("err = %v, want %q", err, tc.message)
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
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			fmt.Fprintf(w, `{"paging":{"page":%d,"size":1,"total":5},"servers":[{"id":"%d","name":"S%d"}]}`,
				page, page+1, page+1)
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
			if p := r.URL.Query().Get("page"); p == "" || p == "0" {
				calls.Add(1)
				fmt.Fprint(w, `{"paging":{"page":0,"size":1,"total":1},"servers":[{"id":"1","name":"One"}]}`)
			} else {
				fmt.Fprint(w, `{"paging":{"page":1,"size":1,"total":1},"servers":[]}`)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setupCreds(t, srv.URL)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := ListServers(context.Background()); err != nil {
				t.Errorf("ListServers: %v", err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
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
			fmt.Fprint(w, `{"paging":{"page":0,"size":1,"total":1},"servers":[{"id":"1","name":"One"}]}`)
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
