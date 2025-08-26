package telemetry

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	logx "modsentinel/internal/logx"
)

func TestHTTPMiddlewareLogs(t *testing.T) {
	var buf bytes.Buffer
	log.Logger = zerolog.New(logx.NewRedactor(&buf)).With().Timestamp().Logger()
	handler := HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
	out := buf.String()
	if !strings.Contains(out, "\"event\":\"http_request\"") {
		t.Fatalf("expected http_request event, got %s", out)
	}
	if !strings.Contains(out, "\"status\":\"418\"") {
		t.Fatalf("expected status 418, got %s", out)
	}
}
