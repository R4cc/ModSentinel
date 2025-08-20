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
	if c.BaseURL == "" {
		return "", time.Time{}, errors.New("base url required")
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
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", time.Time{}, errors.New(resp.Status)
	}
	var res struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
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
	creds, err := Get()
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

// resetToken clears the cached token.
func resetToken() {
	tokenMu.Lock()
	cachedToken = ""
	tokenExpiry = time.Time{}
	tokenMu.Unlock()
}
