package pufferpanel

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	dbpkg "modsentinel/internal/db"
	"modsentinel/internal/secrets"

	_ "modernc.org/sqlite"
)

func setupFiles(t *testing.T) {
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
		t.Fatalf("load keys: %v", err)
	}
	Init(secrets.NewService(db, km))
}

func TestListJarFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case r.URL.Path == "/api/servers/1/files/list" && r.URL.Query().Get("path") == "mods":
			http.NotFound(w, r)
		case r.URL.Path == "/api/servers/1/files/list" && r.URL.Query().Get("path") == "plugins":
			fmt.Fprint(w, `[{"name":"a.jar","is_dir":false},{"name":"b.txt","is_dir":false}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	setupFiles(t)
	if err := Set(Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	jars, err := ListJarFiles(context.Background(), "1")
	if err != nil {
		t.Fatalf("ListJarFiles: %v", err)
	}
	if len(jars) != 1 || jars[0] != "a.jar" {
		t.Fatalf("unexpected jars %v", jars)
	}
}

func TestFetchFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case r.URL.Path == "/api/servers/1/files/contents" && r.URL.Query().Get("path") == "mods/a.jar":
			fmt.Fprint(w, "data")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	setupFiles(t)
	if err := Set(Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	b, err := FetchFile(context.Background(), "1", "mods/a.jar")
	if err != nil {
		t.Fatalf("FetchFile: %v", err)
	}
	if string(b) != "data" {
		t.Fatalf("unexpected data %s", b)
	}
}

func TestListJarFilesErrors(t *testing.T) {
	statuses := []int{http.StatusForbidden, http.StatusInternalServerError}
	for _, code := range statuses {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/oauth2/token":
					fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
				case r.URL.Path == "/api/servers/1/files/list":
					w.WriteHeader(code)
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()

			setupFiles(t)
			if err := Set(Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
				t.Fatalf("Set: %v", err)
			}
			_, err := ListJarFiles(context.Background(), "1")
			want := fmt.Sprintf("%d %s", code, http.StatusText(code))
			if err == nil || err.Error() != want {
				t.Fatalf("err = %v, want %s", err, want)
			}
		})
	}
}
