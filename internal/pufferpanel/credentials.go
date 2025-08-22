package pufferpanel

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"sync/atomic"

	"modsentinel/internal/secrets"
)

// Credentials represents stored PufferPanel credentials.
type Credentials struct {
	BaseURL      string `json:"base_url"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	DeepScan     bool   `json:"deep_scan"`
}

var (
	svc     *secrets.Service
	baseURL atomic.Value // string
)

// Init sets the secrets service used for credential storage.
func Init(s *secrets.Service) { svc = s }

// Set stores the credentials securely.
func Set(c Credentials) error {
	if svc == nil {
		return nil
	}
	if err := validateCreds(&c); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	resetToken()
	baseURL.Store(parseHost(c.BaseURL))
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
	baseURL.Store("")
	return svc.Delete(context.Background(), "pufferpanel")
}

// TestConnection attempts to authenticate against PufferPanel using the provided credentials.
func TestConnection(ctx context.Context, c Credentials) error {
	if err := validateCreds(&c); err != nil {
		return err
	}
	_, _, err := fetchToken(ctx, c)
	return err
}

func parseHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func validateCreds(c *Credentials) error {
	if c.BaseURL == "" {
		return &ConfigError{Reason: "base_url required"}
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return &ConfigError{Reason: "invalid base_url"}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return &ConfigError{Reason: "invalid base_url scheme"}
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawQuery = ""
	u.Fragment = ""
	c.BaseURL = u.String()
	if c.ClientID == "" {
		return &ConfigError{Reason: "client_id required"}
	}
	if c.ClientSecret == "" {
		return &ConfigError{Reason: "client_secret required"}
	}
	return nil
}

func getCreds() (Credentials, error) {
	c, err := Get()
	if err != nil {
		return Credentials{}, err
	}
	if err := validateCreds(&c); err != nil {
		return Credentials{}, err
	}
	return c, nil
}

// APIHost returns the PufferPanel base URL host for CSP connect-src directives.
func APIHost() string {
	if v := baseURL.Load(); v != nil {
		if s, ok := v.(string); ok {
			if s != "" {
				return s
			}
		}
	}
	c, err := Get()
	if err != nil {
		return ""
	}
	h := parseHost(c.BaseURL)
	baseURL.Store(h)
	return h
}
