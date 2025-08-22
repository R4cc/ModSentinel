package telemetry

import (
	"net/http"
	"strconv"
	"time"
)

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// HTTP logs a telemetry event for each HTTP request.
func HTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(sw, r)
		Event("http_request", map[string]string{
			"method": r.Method,
			"path":   r.URL.Path,
			"status": strconv.Itoa(sw.status),
			"ms":     strconv.FormatInt(time.Since(start).Milliseconds(), 10),
		})
	})
}
