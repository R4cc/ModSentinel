package pufferpanel

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"

	"modsentinel/internal/oauth"
	"modsentinel/internal/secrets"
	"modsentinel/internal/settings"
)

// Credentials represents stored PufferPanel credentials.
type Credentials struct {
	BaseURL      string `json:"base_url"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Scopes       string `json:"scopes"`
	DeepScan     bool   `json:"deep_scan"`
}

const defaultScopes = "server.view server.files.view server.files.edit"

const (
	baseURLKey      = "puffer.base_url"
	scopesKey       = "puffer.scopes"
	deepScanKey     = "puffer.deep_scan"
	clientIDKey     = "puffer.oauth_client_id"
	clientSecretKey = "puffer.oauth_client_secret"
)

var (
	secSvc  *secrets.Service
	cfgSvc  *settings.Store
	baseURL atomic.Value // string
)

// Init sets the services used for credential storage.
func Init(sec *secrets.Service, cfg *settings.Store, tok *oauth.Service) {
	secSvc = sec
	cfgSvc = cfg
	tokSvc = tok
}

// Set stores the credentials securely.
func Set(c Credentials) error {
	if secSvc == nil || cfgSvc == nil {
		return nil
	}
	if err := validateCreds(&c); err != nil {
		return err
	}
	ctx := context.Background()
	if err := cfgSvc.Set(ctx, baseURLKey, c.BaseURL); err != nil {
		return err
	}
	if err := cfgSvc.Set(ctx, scopesKey, c.Scopes); err != nil {
		return err
	}
	if err := cfgSvc.Set(ctx, deepScanKey, strconv.FormatBool(c.DeepScan)); err != nil {
		return err
	}
	if err := secSvc.Set(ctx, clientIDKey, []byte(c.ClientID)); err != nil {
		return err
	}
	if err := secSvc.Set(ctx, clientSecretKey, []byte(c.ClientSecret)); err != nil {
		return err
	}
	resetToken()
	baseURL.Store(parseHost(c.BaseURL))
	return nil
}

// Get retrieves stored credentials for internal use.
func Get() (Credentials, error) {
	if secSvc == nil || cfgSvc == nil {
		return Credentials{}, nil
	}
	ctx := context.Background()
	base, err := cfgSvc.Get(ctx, baseURLKey)
	if err != nil {
		return Credentials{}, err
	}
	scopes, err := cfgSvc.Get(ctx, scopesKey)
	if err != nil {
		return Credentials{}, err
	}
	deepStr, err := cfgSvc.Get(ctx, deepScanKey)
	if err != nil {
		return Credentials{}, err
	}
	idb, err := secSvc.Get(ctx, clientIDKey)
	if err != nil {
		return Credentials{}, err
	}
	secb, err := secSvc.Get(ctx, clientSecretKey)
	if err != nil {
		return Credentials{}, err
	}
	c := Credentials{BaseURL: base, ClientID: string(idb), ClientSecret: string(secb), Scopes: scopes, DeepScan: deepStr == "true"}
	if c.BaseURL == "" && c.ClientID == "" && c.ClientSecret == "" {
		return Credentials{}, nil
	}
	origBase := base
	origScopes := scopes
	origDeep := deepStr
	if err := validateCreds(&c); err != nil {
		return Credentials{}, err
	}
	if c.BaseURL != origBase {
		if err := cfgSvc.Set(ctx, baseURLKey, c.BaseURL); err != nil {
			return Credentials{}, err
		}
	}
	if c.Scopes != origScopes {
		if err := cfgSvc.Set(ctx, scopesKey, c.Scopes); err != nil {
			return Credentials{}, err
		}
	}
	if strconv.FormatBool(c.DeepScan) != origDeep {
		if err := cfgSvc.Set(ctx, deepScanKey, strconv.FormatBool(c.DeepScan)); err != nil {
			return Credentials{}, err
		}
	}
	baseURL.Store(parseHost(c.BaseURL))
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
	c.ClientID = ""
	c.ClientSecret = ""
	return c, nil
}

// Exists reports whether credentials are stored.
func Exists() (bool, error) {
	if secSvc == nil {
		return false, nil
	}
	return secSvc.Exists(context.Background(), clientSecretKey)
}

// Clear removes stored credentials.
func Clear() error {
	if secSvc == nil || cfgSvc == nil {
		return nil
	}
	ctx := context.Background()
	resetToken()
	baseURL.Store("")
	secSvc.Delete(ctx, clientIDKey)
	secSvc.Delete(ctx, clientSecretKey)
	cfgSvc.Delete(ctx, baseURLKey)
	cfgSvc.Delete(ctx, scopesKey)
	cfgSvc.Delete(ctx, deepScanKey)
	return nil
}

// TestConnection attempts to authenticate against PufferPanel using the provided credentials.
func TestConnection(ctx context.Context, c Credentials) error {
	if err := validateCreds(&c); err != nil {
		return err
	}
	_, _, _, err := fetchToken(ctx, c, "")
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
	if strings.TrimSpace(c.Scopes) == "" {
		c.Scopes = defaultScopes
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
