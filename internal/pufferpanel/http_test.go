package pufferpanel

import (
	"net/http"
	"net/url"
	"testing"
	"time"
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
