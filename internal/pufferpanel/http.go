package pufferpanel

import (
	"context"
	"io"
	"net/http"

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
