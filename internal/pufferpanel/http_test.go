package pufferpanel

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	logx "modsentinel/internal/logx"
)

func TestNewClientTimeouts(t *testing.T) {
	base, err := url.Parse("https://example.com")
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}
	c := newClient(base)
	if c.Timeout != 30*time.Second {
		t.Fatalf("Timeout = %v, want 30s", c.Timeout)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T, want *http.Transport", c.Transport)
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
}

func TestDoRequestRedactsHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"next":"https://example.com/api"}`))
	}))
	defer srv.Close()

	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}
	client := newClient(base)
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new req: %v", err)
	}

	var buf bytes.Buffer
	old := log.Logger
	log.Logger = zerolog.New(logx.NewRedactor(&buf)).With().Timestamp().Logger()
	defer func() { log.Logger = old }()

	if _, _, err := doRequest(context.Background(), client, req); err != nil {
		t.Fatalf("doRequest: %v", err)
	}
	if strings.Contains(buf.String(), "example.com") {
		t.Fatalf("log contains host: %s", buf.String())
	}
}

func TestNewClientRedirectRewritesHost(t *testing.T) {
	var externalCalled atomic.Bool
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		externalCalled.Store(true)
		t.Fatalf("external host contacted: %s", r.URL.String())
	}))
	defer external.Close()

	base := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, external.URL+"/next", http.StatusFound)
			return
		}
		if r.URL.Path == "/next" {
			w.WriteHeader(http.StatusOK)
			return
		}
		t.Fatalf("unexpected path: %s", r.URL.Path)
	}))
	defer base.Close()

	u, err := url.Parse(base.URL)
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}
	client := newClient(u)
	resp, err := client.Get(base.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	resp.Body.Close()
	if externalCalled.Load() {
		t.Fatalf("external host was contacted")
	}
}

func TestNewClientIgnoresProxy(t *testing.T) {
	var proxyCalled atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalled.Store(true)
		t.Fatalf("proxy was used for %s", r.URL.String())
	}))
	defer proxy.Close()

	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)

	baseCalled := atomic.Bool{}
	base := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		baseCalled.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer base.Close()

	u, err := url.Parse(base.URL)
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}
	client := newClient(u)
	resp, err := client.Get(base.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	resp.Body.Close()
	if proxyCalled.Load() {
		t.Fatalf("proxy server was contacted")
	}
	if !baseCalled.Load() {
		t.Fatalf("base server was not contacted")
	}
}
