package pufferpanel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"modsentinel/internal/telemetry"
)

// Server represents a PufferPanel server.
type Server struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type paging struct {
	Page  int    `json:"page"`
	Size  int    `json:"size"`
	Total int    `json:"total"`
	Next  string `json:"next"`
}

type serverList struct {
	Paging  paging   `json:"paging"`
	Servers []Server `json:"servers"`
}

var (
	serverGroup singleflight.Group
	maxServers  = 1000
	serverTTL   = 2 * time.Second
	serverCache sync.Map // map[baseURL]cacheEntry
)

type cacheEntry struct {
	servers []Server
	exp     time.Time
}

// ListServers fetches available servers from PufferPanel.
func ListServers(ctx context.Context) (servers []Server, err error) {
	start := time.Now()
	cacheHit := false
	deduped := false
	defer func() {
		status := "ok"
		if err != nil {
			status = "error"
		}
		telemetry.Event("pufferpanel_request", map[string]string{
			"resource":    "pufferpanel.servers",
			"status":      status,
			"duration_ms": strconv.FormatInt(time.Since(start).Milliseconds(), 10),
			"deduped":     strconv.FormatBool(deduped),
			"cache_hit":   strconv.FormatBool(cacheHit),
		})
	}()

	creds, err := getCreds()
	if err != nil {
		return nil, err
	}
	if v, ok := serverCache.Load(creds.BaseURL); ok {
		ent := v.(cacheEntry)
		if time.Now().Before(ent.exp) {
			cacheHit = true
			return ent.servers, nil
		}
	}
	var shared bool
	var v any
	v, err, shared = serverGroup.Do(creds.BaseURL, func() (any, error) {
		svs, err := fetchServers(ctx, creds)
		if err != nil {
			return nil, err
		}
		serverCache.Store(creds.BaseURL, cacheEntry{servers: svs, exp: time.Now().Add(serverTTL)})
		return svs, nil
	})
	deduped = shared
	if err != nil {
		return nil, err
	}
	servers = v.([]Server)
	return servers, nil
}

func fetchServers(ctx context.Context, creds Credentials) ([]Server, error) {
	base, err := url.Parse(creds.BaseURL)
	if err != nil {
		return nil, err
	}
	nextURL := *base
	nextURL.Path = strings.TrimSuffix(nextURL.Path, "/") + "/api/servers"
	client := newClient(base)
	var all []Server
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL.String(), nil)
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
		if len(all) >= res.Paging.Total || len(res.Servers) == 0 || len(all) >= maxServers || res.Paging.Next == "" {
			if len(all) > maxServers {
				all = all[:maxServers]
			}
			break
		}
		u, err := url.Parse(res.Paging.Next)
		if err != nil {
			return nil, err
		}
		if u.IsAbs() {
			u.Scheme = base.Scheme
			u.Host = base.Host
		} else {
			u = base.ResolveReference(u)
		}
		nextURL = *u
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
	client := newClient(u)
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
