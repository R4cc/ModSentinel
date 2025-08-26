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

	"github.com/rs/zerolog/log"

	"modsentinel/internal/oauth"
)

var (
	tokenMu     sync.Mutex
	cachedToken string
	tokenExpiry time.Time

	tokSvc *oauth.Service
)

// fetchToken retrieves a new access token. If refresh is empty it performs the
// client credentials flow; otherwise it attempts a refresh_token grant.
func fetchToken(ctx context.Context, c Credentials, refresh string) (access, newRefresh string, exp time.Time, err error) {
	if err = validateCreds(&c); err != nil {
		return
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/oauth2/token"
	data := url.Values{
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
	}
	if refresh == "" {
		data.Set("grant_type", "client_credentials")
		if c.Scopes != "" {
			data.Set("scope", c.Scopes)
		}
	} else {
		data.Set("grant_type", "refresh_token")
		data.Set("refresh_token", refresh)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(data.Encode()))
	if err != nil {
		return "", "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := newClient(u)
	status, body, err := doRequest(ctx, client, req)
	if err != nil {
		return "", "", time.Time{}, err
	}
	if status < 200 || status >= 300 {
		if status >= http.StatusInternalServerError {
			return "", "", time.Time{}, parseError(status, body)
		}
		var oe struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		if json.Unmarshal(body, &oe) == nil && oe.Error != "" {
			switch oe.Error {
			case "invalid_client":
				return "", "", time.Time{}, &Error{Status: http.StatusUnauthorized, Message: "invalid client credentials"}
			case "invalid_scope":
				return "", "", time.Time{}, &Error{Status: http.StatusForbidden, Message: "invalid scope"}
			default:
				msg := oe.Description
				if msg == "" {
					msg = oe.Error
				}
				return "", "", time.Time{}, &Error{Status: status, Message: msg}
			}
		}
		return "", "", time.Time{}, parseError(status, body)
	}
	var res struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return "", "", time.Time{}, err
	}
	if res.AccessToken == "" {
		return "", "", time.Time{}, errors.New("missing access_token")
	}
	exp = time.Now().Add(time.Duration(res.ExpiresIn) * time.Second)
	return res.AccessToken, res.RefreshToken, exp, nil
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
	var rec oauth.Record
	if tokSvc != nil {
		rec, err = tokSvc.Get(ctx, "pufferpanel")
		if err != nil {
			return "", err
		}
	}
	if rec.AccessToken != "" {
		if time.Now().Before(rec.Expiry.Add(-10 * time.Second)) {
			cachedToken = rec.AccessToken
			tokenExpiry = rec.Expiry
			return cachedToken, nil
		}
		if rec.RefreshToken != "" {
			at, rt, exp, err := fetchToken(ctx, creds, rec.RefreshToken)
			if err == nil {
				cachedToken = at
				tokenExpiry = exp
				if tokSvc != nil {
					tokSvc.Store(ctx, "pufferpanel", oauth.Record{Subject: rec.Subject, Scope: creds.Scopes, AccessToken: at, RefreshToken: rt, Expiry: exp})
				}
				return cachedToken, nil
			}
			log.Error().Err(err).Msg("refresh pufferpanel token")
		}
	}
	at, rt, exp, err := fetchToken(ctx, creds, "")
	if err != nil {
		return "", err
	}
	cachedToken = at
	tokenExpiry = exp
	if tokSvc != nil {
		if err := tokSvc.Store(ctx, "pufferpanel", oauth.Record{Scope: creds.Scopes, AccessToken: at, RefreshToken: rt, Expiry: exp}); err != nil {
			log.Error().Err(err).Msg("store pufferpanel token")
		}
	}
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
	if tokSvc != nil {
		tokSvc.Clear(context.Background(), "pufferpanel")
	}
}

// StartRefresh launches a background goroutine that refreshes the stored
// OAuth tokens five minutes before expiry. Repeated failures back off
// exponentially.
func StartRefresh(ctx context.Context) {
	if tokSvc == nil {
		return
	}
	go func() {
		backoff := time.Second
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			rec, err := tokSvc.Get(ctx, "pufferpanel")
			if err != nil || rec.AccessToken == "" || rec.RefreshToken == "" {
				time.Sleep(time.Minute)
				continue
			}
			wait := time.Until(rec.Expiry.Add(-5 * time.Minute))
			if wait > 0 {
				select {
				case <-time.After(wait):
				case <-ctx.Done():
					return
				}
			}
			creds, err := getCreds()
			if err != nil {
				log.Error().Err(err).Msg("pufferpanel creds for refresh")
				time.Sleep(backoff)
				if backoff < time.Minute*10 {
					backoff *= 2
				}
				continue
			}
			at, rt, exp, err := fetchToken(ctx, creds, rec.RefreshToken)
			if err != nil {
				log.Error().Err(err).Msg("refresh pufferpanel token")
				time.Sleep(backoff)
				if backoff < time.Minute*10 {
					backoff *= 2
				}
				continue
			}
			backoff = time.Second
			tokSvc.Store(ctx, "pufferpanel", oauth.Record{Subject: rec.Subject, Scope: creds.Scopes, AccessToken: at, RefreshToken: rt, Expiry: exp})
		}
	}()
}
