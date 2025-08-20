package pufferpanel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
)

// Credentials represents stored PufferPanel credentials.
type Credentials struct {
	BaseURL      string `json:"base_url"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	DeepScan     bool   `json:"deep_scan"`
}

func credsPath() (string, error) {
	if p := os.Getenv("MODSENTINEL_PUFFERPANEL_PATH"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "modsentinel", "pufferpanel_creds"), nil
}

// Set stores the credentials persistently.
func Set(c Credentials) error {
	p, err := credsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	resetToken()
	return os.WriteFile(p, b, 0o600)
}

// Get retrieves stored credentials.
func Get() (Credentials, error) {
	p, err := credsPath()
	if err != nil {
		return Credentials{}, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return Credentials{}, nil
	}
	if err != nil {
		return Credentials{}, err
	}
	var c Credentials
	if err := json.Unmarshal(b, &c); err != nil {
		return Credentials{}, err
	}
	return c, nil
}

// Clear removes stored credentials.
func Clear() error {
	p, err := credsPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	resetToken()
	return nil
}

// TestConnection attempts to authenticate against PufferPanel using the provided credentials.
func TestConnection(ctx context.Context, c Credentials) error {
	_, _, err := fetchToken(ctx, c)
	return err
}
