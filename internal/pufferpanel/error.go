package pufferpanel

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Error represents an error response from PufferPanel.
type Error struct {
	Status    int             `json:"-"`
	Code      int             `json:"code"`
	Message   string          `json:"message"`
	Details   json.RawMessage `json:"details,omitempty"`
	RequestID string          `json:"requestId"`
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return http.StatusText(e.Status)
}

// parseError constructs an Error from status and body.
func parseError(status int, body []byte) error {
	e := &Error{Status: status}
	if err := json.Unmarshal(body, e); err == nil {
		if e.Message == "" {
			e.Message = http.StatusText(status)
		}
		return e
	}
	e.Message = strings.TrimSpace(string(body))
	if e.Message == "" {
		e.Message = http.StatusText(status)
	}
	return e
}

// ConfigError represents a configuration problem before reaching PufferPanel.
type ConfigError struct{ Reason string }

func (e *ConfigError) Error() string { return e.Reason }
