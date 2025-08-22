package pufferpanel

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/rs/zerolog/log"
)

// doRequest performs the HTTP request and logs the upstream response.
func doRequest(ctx context.Context, client *http.Client, req *http.Request) (int, []byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	logBody := body
	if len(logBody) > 1024 {
		logBody = logBody[:1024]
	}
	log.Ctx(ctx).Info().
		Str("requestId", requestIDFromContext(ctx)).
		Int("upstream_code", resp.StatusCode).
		Str("upstream_body", string(logBody)).
		Msg("pufferpanel response")
	return resp.StatusCode, body, err
}

// newClient creates an HTTP client that rewrites redirect destinations to the base host.
func newClient(base *url.URL) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSHandshakeTimeout = 5 * time.Second
	transport.ResponseHeaderTimeout = 10 * time.Second
	transport.ExpectContinueTimeout = 1 * time.Second
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			req.URL.Scheme = base.Scheme
			req.URL.Host = base.Host
			return nil
		},
	}
}
