package logx

import (
	"fmt"
	"io"
	"regexp"
	"strings"
)

var fieldRE = regexp.MustCompile(`(?i)"([^"\\]*?(token|secret|password|key)[^"\\]*)":"[^"]*"`)

// NewRedactor returns a writer that redacts token or secret values.
func NewRedactor(w io.Writer) io.Writer {
	return &redactor{w: w}
}

type redactor struct {
	w io.Writer
}

func (r *redactor) Write(p []byte) (int, error) {
	s := fieldRE.ReplaceAllStringFunc(string(p), func(m string) string {
		parts := strings.SplitN(m, ":", 2)
		if len(parts) != 2 {
			return m
		}
		return parts[0] + ":\"***redacted***\""
	})
	return r.w.Write([]byte(s))
}

// Secret returns a placeholder for a sensitive value, preserving its length.
func Secret(val string) string {
	if val == "" {
		return ""
	}
	return fmt.Sprintf("***redacted*** (%d)", len(val))
}
