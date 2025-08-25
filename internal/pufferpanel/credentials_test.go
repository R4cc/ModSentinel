package pufferpanel

import (
	"context"
	"encoding/json"
	"fmt"
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
	raw := fmt.Sprintf(`{"base_url":"%s/","client_id":"id","client_secret":"secret"}`, srv.URL)
	if err := svc.Set(context.Background(), "pufferpanel", []byte(raw)); err != nil {
		t.Fatalf("seed creds: %v", err)
	}
	c, err := Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if c.BaseURL != srv.URL {
		t.Fatalf("BaseURL = %s, want %s", c.BaseURL, srv.URL)
	}
	b, err := svc.DecryptForUse(context.Background(), "pufferpanel")
	if err != nil {
		t.Fatalf("DecryptForUse: %v", err)
	}
	var stored Credentials
	if err := json.Unmarshal(b, &stored); err != nil {
		t.Fatalf("unmarshal stored: %v", err)
	}
	if stored.BaseURL != srv.URL {
		t.Fatalf("stored BaseURL = %s, want %s", stored.BaseURL, srv.URL)
	}
	if stored.Scopes != defaultScopes {
		t.Fatalf("stored Scopes = %q, want %q", stored.Scopes, defaultScopes)
	}
}
