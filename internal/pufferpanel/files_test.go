package pufferpanel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	dbpkg "modsentinel/internal/db"
	"modsentinel/internal/oauth"
	"modsentinel/internal/secrets"
	"modsentinel/internal/settings"

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
	if err := dbpkg.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	svc := secrets.NewService(db)
	cfg := settings.New(db)
	oauthSvc := oauth.New(db)
	Init(svc, cfg, oauthSvc)
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
	cases := []struct {
		status  int
		message string
	}{
		{http.StatusForbidden, "nope"},
		{http.StatusInternalServerError, "broken"},
	}
	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/oauth2/token":
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
				case r.URL.Path == "/api/servers/1/files/list":
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tc.status)
					fmt.Fprintf(w, `{"code":%d,"message":"%s","requestId":"x"}`, tc.status, tc.message)
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

func TestListPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case strings.HasPrefix(r.URL.Path, "/api/servers/1/file/"):
			gotPath = r.URL.Path
			fmt.Fprint(w, `[{"name":"a.jar","is_dir":false}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	setupFiles(t)
	if err := Set(Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	files, err := ListPath(context.Background(), "1", "mods/a.jar")
	if err != nil {
		t.Fatalf("ListPath: %v", err)
	}
	if gotPath != "/api/servers/1/file/mods%2Fa.jar" {
		t.Fatalf("path = %s", gotPath)
	}
	if len(files) != 1 || files[0].Name != "a.jar" {
		t.Fatalf("unexpected files %+v", files)
	}
}

func TestListPathMissingMods(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case r.URL.Path == "/api/servers/1/file/mods%2F":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	setupFiles(t)
	if err := Set(Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	_, err := ListPath(context.Background(), "1", "mods/")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestPutFile(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/servers/1/file/"):
			gotPath = r.URL.Path
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	setupFiles(t)
	if err := Set(Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := PutFile(context.Background(), "1", "mods/a.jar", []byte("data")); err != nil {
		t.Fatalf("PutFile: %v", err)
	}
	if gotPath != "/api/servers/1/file/mods%2Fa.jar" {
		t.Fatalf("path = %s", gotPath)
	}
	if gotBody != "data" {
		t.Fatalf("body = %s", gotBody)
	}
}

func TestDeleteFile(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/servers/1/file/"):
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	setupFiles(t)
	if err := Set(Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := DeleteFile(context.Background(), "1", "mods/a.jar"); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if gotPath != "/api/servers/1/file/mods%2Fa.jar" {
		t.Fatalf("path = %s", gotPath)
	}
}

func TestFileOpErrors(t *testing.T) {
	cases := []struct {
		status int
		call   func(context.Context) error
		want   error
	}{
		{http.StatusForbidden, func(ctx context.Context) error {
			return PutFile(ctx, "1", "a", []byte("x"))
		}, ErrForbidden},
		{http.StatusNotFound, func(ctx context.Context) error {
			_, err := ListPath(ctx, "1", "a")
			return err
		}, ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/oauth2/token":
					fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
				case strings.HasPrefix(r.URL.Path, "/api/servers/1/file/"):
					w.WriteHeader(tc.status)
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()

			setupFiles(t)
			if err := Set(Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
				t.Fatalf("Set: %v", err)
			}
			err := tc.call(context.Background())
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
