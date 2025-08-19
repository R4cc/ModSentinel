package token

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTokenStorage(t *testing.T) {
	t.Setenv("MODSENTINEL_TOKEN_PATH", filepath.Join(t.TempDir(), "token"))
	tok := "abcdef123456"
	if err := SetToken(tok); err != nil {
		t.Fatalf("set token: %v", err)
	}
	got, err := GetToken()
	if err != nil {
		t.Fatalf("get token: %v", err)
	}
	if got != tok {
		t.Fatalf("got %q want %q", got, tok)
	}
	if err := ClearToken(); err != nil {
		t.Fatalf("clear token: %v", err)
	}
	got, err = GetToken()
	if err != nil {
		t.Fatalf("get after clear: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty token, got %q", got)
	}
}

func TestTokenRedaction(t *testing.T) {
	t.Setenv("MODSENTINEL_TOKEN_PATH", filepath.Join(t.TempDir(), "token"))
	tok := "abcdef1234567890"
	if err := SetToken(tok); err != nil {
		t.Fatalf("set token: %v", err)
	}
	stored, redacted, err := TokenForLog()
	if err != nil {
		t.Fatalf("token for log: %v", err)
	}
	if stored != tok {
		t.Fatalf("stored token mismatch: got %q want %q", stored, tok)
	}
	if stored == redacted {
		t.Fatalf("redacted token matches original")
	}
	middle := tok[4 : len(tok)-4]
	if strings.Contains(redacted, middle) {
		t.Fatalf("redacted token reveals middle: %q", redacted)
	}
	if !strings.HasPrefix(redacted, tok[:4]) || !strings.HasSuffix(redacted, tok[len(tok)-4:]) {
		t.Fatalf("redacted token missing expected prefix or suffix: %q", redacted)
	}
}
