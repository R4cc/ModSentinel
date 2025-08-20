package pufferpanel

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func setupCreds(t *testing.T, base string) {
	dir := t.TempDir()
	os.Setenv("MODSENTINEL_PUFFERPANEL_PATH", filepath.Join(dir, "creds"))
	t.Cleanup(func() { os.Unsetenv("MODSENTINEL_PUFFERPANEL_PATH") })
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
			fmt.Fprint(w, `[{"id":"1","name":"One"}]`)
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
	if len(svs) != 1 || svs[0].ID != "1" {
		t.Fatalf("unexpected servers %+v", svs)
	}
}

func TestListServersErrors(t *testing.T) {
	statuses := []int{http.StatusForbidden, http.StatusInternalServerError}
	for _, code := range statuses {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/oauth2/token":
					fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
				case "/api/servers":
					w.WriteHeader(code)
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			setupCreds(t, srv.URL)
			_, err := ListServers(context.Background())
			want := fmt.Sprintf("%d %s", code, http.StatusText(code))
			if err == nil || err.Error() != want {
				t.Fatalf("err = %v, want %s", err, want)
			}
		})
	}
}
