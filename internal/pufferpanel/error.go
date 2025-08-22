package pufferpanel

import (
	"encoding/json"
	"io"
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

// parseError reads the response body and returns an Error.
func parseError(resp *http.Response) error {
	defer resp.Body.Close()
	e := &Error{Status: resp.StatusCode}
	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(resp.Body).Decode(e); err == nil {
			if e.Message == "" {
				e.Message = http.StatusText(resp.StatusCode)
			}
			return e
		}
	}
	b, _ := io.ReadAll(resp.Body)
	e.Message = strings.TrimSpace(string(b))
	if e.Message == "" {
		e.Message = http.StatusText(resp.StatusCode)
	}
	return e
}
