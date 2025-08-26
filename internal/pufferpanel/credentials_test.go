package pufferpanel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSetValidatesAndNormalizesBaseURL(t *testing.T) {
	setup(t)
	if err := Set(Credentials{BaseURL: "ftp://foo", ClientID: "id", ClientSecret: "secret"}); err == nil {
		t.Fatalf("expected error for invalid scheme")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	if err := Set(Credentials{BaseURL: srv.URL + "/", ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	c, err := Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if c.BaseURL != srv.URL {
		t.Fatalf("base URL = %s, want %s", c.BaseURL, srv.URL)
	}
	if c.Scopes != defaultScopes {
		t.Fatalf("scopes = %q, want %q", c.Scopes, defaultScopes)
	}
}

func TestGetNormalizesExistingBaseURL(t *testing.T) {
	setup(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	ctx := context.Background()
	if err := cfgSvc.Set(ctx, baseURLKey, srv.URL+"/"); err != nil {
		t.Fatalf("seed base: %v", err)
	}
	if err := secSvc.Set(ctx, clientIDKey, []byte("id")); err != nil {
		t.Fatalf("seed id: %v", err)
	}
	if err := secSvc.Set(ctx, clientSecretKey, []byte("secret")); err != nil {
		t.Fatalf("seed secret: %v", err)
	}
	c, err := Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if c.BaseURL != srv.URL {
		t.Fatalf("BaseURL = %s, want %s", c.BaseURL, srv.URL)
	}
	storedBase, err := cfgSvc.Get(ctx, baseURLKey)
	if err != nil {
		t.Fatalf("Get base: %v", err)
	}
	if storedBase != srv.URL {
		t.Fatalf("stored BaseURL = %s, want %s", storedBase, srv.URL)
	}
	storedScopes, err := cfgSvc.Get(ctx, scopesKey)
	if err != nil {
		t.Fatalf("Get scopes: %v", err)
	}
	if storedScopes != defaultScopes {
		t.Fatalf("stored Scopes = %q, want %q", storedScopes, defaultScopes)
	}
}
