package pufferpanel

import (
	"context"
	"encoding/json"
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

type paging struct {
	Page  int `json:"page"`
	Size  int `json:"size"`
	Total int `json:"total"`
}

type serverList struct {
	Paging  paging   `json:"paging"`
	Servers []Server `json:"servers"`
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
	creds, err := getCreds()
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(creds.BaseURL)
	if err != nil {
		return nil, err
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/api/servers"
	client := &http.Client{Timeout: 10 * time.Second}
	var all []Server
	// PufferPanel pagination starts at 1, so begin with page 1.
	for page := 1; ; page++ {
		reqURL := *u
		q := reqURL.Query()
		q.Set("page", strconv.Itoa(page))
		reqURL.RawQuery = q.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
		if err != nil {
			return nil, err
		}
		status, body, err := doAuthRequest(ctx, client, req)
		if err != nil {
			return nil, err
		}
		if status < 200 || status >= 300 {
			return nil, parseError(status, body)
		}
		var res serverList
		if err := json.Unmarshal(body, &res); err != nil {
			return nil, err
		}
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
	creds, err := getCreds()
	if err != nil {
		return nil, err
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
	client := &http.Client{Timeout: 10 * time.Second}
	status, body, err := doAuthRequest(ctx, client, req)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, parseError(status, body)
	}
	var d ServerDetail
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, err
	}
	return &d, nil
}
