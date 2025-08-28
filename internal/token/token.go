package token

import (
	"context"

	"modsentinel/internal/logx"
	"modsentinel/internal/secrets"
)

var svc *secrets.Service

// Init sets the secrets service to use for token operations.
func Init(s *secrets.Service) { svc = s }

// SetToken stores the Modrinth API token.
func SetToken(token string) error {
	if svc == nil {
		return nil
	}
	return svc.Set(context.Background(), "modrinth", []byte(token))
}

// GetToken retrieves the Modrinth API token for internal use.
func GetToken() (string, error) {
	if svc == nil {
		return "", nil
	}
	b, err := svc.Get(context.Background(), "modrinth")
	return string(b), err
}

// Exists reports whether a token is stored.
func Exists() (bool, error) {
	if svc == nil {
		return false, nil
	}
	return svc.Exists(context.Background(), "modrinth")
}

// ClearToken removes the stored Modrinth API token.
func ClearToken() error {
	if svc == nil {
		return nil
	}
	return svc.Delete(context.Background(), "modrinth")
}

// TokenForLog returns the current token and a redacted version safe for logging.
func TokenForLog() (string, string, error) {
	tok, err := GetToken()
	if err != nil {
		return "", "", err
	}
	return tok, logx.Secret(tok), nil
}
