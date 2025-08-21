package pufferpanel

import (
	"context"
	"encoding/json"

	"modsentinel/internal/secrets"
)

// Credentials represents stored PufferPanel credentials.
type Credentials struct {
	BaseURL      string `json:"base_url"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	DeepScan     bool   `json:"deep_scan"`
}

var svc *secrets.Service

// Init sets the secrets service used for credential storage.
func Init(s *secrets.Service) { svc = s }

// Set stores the credentials securely.
func Set(c Credentials) error {
	if svc == nil {
		return nil
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	resetToken()
	return svc.Set(context.Background(), "pufferpanel", b)
}

// Get retrieves stored credentials for internal use.
func Get() (Credentials, error) {
	if svc == nil {
		return Credentials{}, nil
	}
	b, err := svc.DecryptForUse(context.Background(), "pufferpanel")
	if err != nil || len(b) == 0 {
		return Credentials{}, err
	}
	var c Credentials
	if err := json.Unmarshal(b, &c); err != nil {
		return Credentials{}, err
	}
	return c, nil
}

// Config returns stored credentials without sensitive fields for HTTP responses.
func Config() (Credentials, error) {
	c, err := Get()
	if err != nil {
		return Credentials{}, err
	}
	if c == (Credentials{}) {
		return c, nil
	}
	c.ClientSecret = ""
	return c, nil
}

// Exists reports whether credentials are stored.
func Exists() (bool, error) {
	if svc == nil {
		return false, nil
	}
	return svc.Exists(context.Background(), "pufferpanel")
}

// Clear removes stored credentials.
func Clear() error {
	if svc == nil {
		return nil
	}
	resetToken()
	return svc.Delete(context.Background(), "pufferpanel")
}

// TestConnection attempts to authenticate against PufferPanel using the provided credentials.
func TestConnection(ctx context.Context, c Credentials) error {
	_, _, err := fetchToken(ctx, c)
	return err
}
