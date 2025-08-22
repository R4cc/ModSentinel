package pufferpanel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"
)

// Server represents a PufferPanel server.
type Server struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

var (
	serverGroup singleflight.Group
	maxServers  = 1000
)

// ListServers fetches available servers from PufferPanel.
func ListServers(ctx context.Context) ([]Server, error) {
	v, err, _ := serverGroup.Do("servers", func() (any, error) {
		return fetchServers(ctx)
	})
	if err != nil {
		return nil, err
	}
	return v.([]Server), nil
}

func fetchServers(ctx context.Context) ([]Server, error) {
	creds, err := Get()
	if err != nil {
		return nil, err
	}
	if creds.BaseURL == "" {
		return nil, errors.New("base url required")
	}
	u, err := url.Parse(creds.BaseURL)
	if err != nil {
		return nil, err
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/api/servers"
	client := &http.Client{Timeout: 10 * time.Second}
	var all []Server
	for page := 0; ; page++ {
		reqURL := *u
		q := reqURL.Query()
		q.Set("page", strconv.Itoa(page))
		reqURL.RawQuery = q.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
		if err != nil {
			return nil, err
		}
		if err := AddAuth(ctx, req); err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, parseError(resp)
		}
		var res struct {
			Paging struct {
				Page  int `json:"page"`
				Size  int `json:"size"`
				Total int `json:"total"`
			} `json:"paging"`
			Servers []Server `json:"servers"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			return nil, err
		}
		resp.Body.Close()
		all = append(all, res.Servers...)
		if len(all) >= res.Paging.Total || len(res.Servers) == 0 || len(all) >= maxServers {
			if len(all) > maxServers {
				all = all[:maxServers]
			}
			break
		}
	}
	return all, nil
}

// ServerDetail includes server info with environment type.
type ServerDetail struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Environment struct {
		Type string `json:"type"`
	} `json:"environment"`
}

// GetServer fetches details for a single server.
func GetServer(ctx context.Context, id string) (*ServerDetail, error) {
	creds, err := Get()
	if err != nil {
		return nil, err
	}
	if creds.BaseURL == "" {
		return nil, errors.New("base url required")
	}
	u, err := url.Parse(creds.BaseURL)
	if err != nil {
		return nil, err
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/api/servers/" + id
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if err := AddAuth(ctx, req); err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseError(resp)
	}
	defer resp.Body.Close()
	var d ServerDetail
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, err
	}
	return &d, nil
}
