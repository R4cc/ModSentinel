package token

import (
	"os"
	"path/filepath"
	"strings"
)

func tokenPath() (string, error) {
	if p := os.Getenv("MODSENTINEL_TOKEN_PATH"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "modsentinel", "modrinth_token"), nil
}

// SetToken writes the Modrinth API token to persistent storage.
func SetToken(token string) error {
	p, err := tokenPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(token), 0o600)
}

// GetToken retrieves the Modrinth API token from persistent storage.
func GetToken() (string, error) {
	p, err := tokenPath()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// ClearToken removes the stored Modrinth API token.
func ClearToken() error {
	p, err := tokenPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// TokenForLog returns the current token and a redacted version safe for logging.
func TokenForLog() (string, string, error) {
	tok, err := GetToken()
	if err != nil {
		return "", "", err
	}
	return tok, redactToken(tok), nil
}

func redactToken(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return strings.Repeat("*", len(s))
	}
	return s[:4] + strings.Repeat("*", len(s)-8) + s[len(s)-4:]
}
