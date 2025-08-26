package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	logx "modsentinel/internal/logx"
)

func TestWriteDoesNotLeakTelemetry(t *testing.T) {
	var logBuf bytes.Buffer
	orig := log.Logger
	log.Logger = zerolog.New(logx.NewRedactor(&logBuf)).With().Timestamp().Logger()
	t.Cleanup(func() { log.Logger = orig })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	Write(rec, req, Internal(errors.New("boom")))

	if strings.Contains(rec.Body.String(), "telemetry") {
		t.Fatalf("telemetry leaked into response: %s", rec.Body.String())
	}
	var errResp Error
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if strings.Contains(errResp.Message, "telemetry") {
		t.Fatalf("telemetry leaked into message: %s", errResp.Message)
	}
	if !strings.Contains(logBuf.String(), "\"event\":\"api_error\"") {
		t.Fatalf("expected api_error log, got %s", logBuf.String())
	}
}
