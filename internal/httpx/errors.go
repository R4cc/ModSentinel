package httpx

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"modsentinel/internal/telemetry"
)

// Error represents a JSON API error response.
type Error struct {
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	RequestID string            `json:"requestId"`
	Details   map[string]string `json:"details,omitempty"`
}

// HTTPError is an error with an associated HTTP status and code.
type HTTPError struct {
	status  int
	code    string
	message string
	details map[string]string
}

func (e *HTTPError) Error() string { return e.message }
func (e *HTTPError) Status() int   { return e.status }
func (e *HTTPError) Code() string  { return e.code }

func (e *HTTPError) WithDetails(d map[string]string) *HTTPError {
	e.details = d
	return e
}

// BadRequest returns a 400 HTTPError.
func BadRequest(msg string) *HTTPError {
	return &HTTPError{status: http.StatusBadRequest, code: "bad_request", message: msg}
}

// Unauthorized returns a 401 HTTPError.
func Unauthorized(msg string) *HTTPError {
	return &HTTPError{status: http.StatusUnauthorized, code: "unauthorized", message: msg}
}

// Forbidden returns a 403 HTTPError.
func Forbidden(msg string) *HTTPError {
	return &HTTPError{status: http.StatusForbidden, code: "forbidden", message: msg}
}

// NotFound returns a 404 HTTPError.
func NotFound(msg string) *HTTPError {
	return &HTTPError{status: http.StatusNotFound, code: "not_found", message: msg}
}

// BadGateway returns a 502 HTTPError.
func BadGateway(msg string) *HTTPError {
	return &HTTPError{status: http.StatusBadGateway, code: "bad_gateway", message: msg}
}

// TooManyRequests returns a 429 HTTPError.
func TooManyRequests(msg string) *HTTPError {
	return &HTTPError{status: http.StatusTooManyRequests, code: "rate_limited", message: msg}
}

// Unavailable returns a 503 HTTPError.
func Unavailable(msg string) *HTTPError {
	return &HTTPError{status: http.StatusServiceUnavailable, code: "service_unavailable", message: msg}
}

// Internal returns a 500 HTTPError.
func Internal(err error) *HTTPError {
	msg := "internal server error"
	if err != nil {
		msg = err.Error()
	}
	return &HTTPError{status: http.StatusInternalServerError, code: "internal_error", message: msg}
}

// Write writes the error to the response writer in JSON format.
func Write(w http.ResponseWriter, r *http.Request, err error) {
	var he *HTTPError
	if errors.As(err, &he) {
		write(w, r, he.status, he.code, he.message, he.details)
		return
	}
	write(w, r, http.StatusInternalServerError, "internal_error", err.Error(), nil)
}

func write(w http.ResponseWriter, r *http.Request, status int, code, msg string, details map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	telemetry.Event("api_error", map[string]string{"status": strconv.Itoa(status), "code": code})
	json.NewEncoder(w).Encode(Error{
		Code:      code,
		Message:   msg,
		RequestID: requestID(r),
		Details:   details,
	})
}

func requestID(r *http.Request) string {
	id := r.Header.Get("X-Request-ID")
	if id != "" {
		return id
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}
