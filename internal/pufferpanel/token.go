package pufferpanel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	tokenMu     sync.Mutex
	cachedToken string
	tokenExpiry time.Time
)

// fetchToken retrieves an access token using the client credentials.
func fetchToken(ctx context.Context, c Credentials) (string, time.Time, error) {
	if err := validateCreds(&c); err != nil {
		return "", time.Time{}, err
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return "", time.Time{}, err
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/oauth2/token"
	data := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(data.Encode()))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := newClient(u)
	status, body, err := doRequest(ctx, client, req)
	if err != nil {
		return "", time.Time{}, err
	}
	if status < 200 || status >= 300 {
		return "", time.Time{}, parseError(status, body)
	}
	var res struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return "", time.Time{}, err
	}
	if res.AccessToken == "" {
		return "", time.Time{}, errors.New("missing access_token")
	}
	exp := time.Now().Add(time.Duration(res.ExpiresIn) * time.Second)
	return res.AccessToken, exp, nil
}

// getToken returns a cached access token or fetches a new one if expired.
func getToken(ctx context.Context) (string, error) {
	tokenMu.Lock()
	defer tokenMu.Unlock()
	if cachedToken != "" && time.Now().Before(tokenExpiry.Add(-10*time.Second)) {
		return cachedToken, nil
	}
	creds, err := getCreds()
	if err != nil {
		return "", err
	}
	tok, exp, err := fetchToken(ctx, creds)
	if err != nil {
		return "", err
	}
	cachedToken = tok
	tokenExpiry = exp
	return cachedToken, nil
}

// AddAuth attaches the Authorization header with a bearer token.
func AddAuth(ctx context.Context, req *http.Request) error {
	tok, err := getToken(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tok))
	return nil
}

// doAuthRequest attaches a bearer token and retries once on 401.
func doAuthRequest(ctx context.Context, client *http.Client, req *http.Request) (int, []byte, error) {
	if err := AddAuth(ctx, req); err != nil {
		return 0, nil, err
	}
	status, body, err := doRequest(ctx, client, req)
	if err != nil {
		return status, body, err
	}
	if status == http.StatusUnauthorized {
		resetToken()
		if err := AddAuth(ctx, req); err != nil {
			return 0, nil, err
		}
		return doRequest(ctx, client, req)
	}
	return status, body, nil
}

// resetToken clears the cached token.
func resetToken() {
	tokenMu.Lock()
	cachedToken = ""
	tokenExpiry = time.Time{}
	tokenMu.Unlock()
}
