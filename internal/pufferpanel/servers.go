package pufferpanel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Server represents a PufferPanel server.
type Server struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListServers fetches available servers from PufferPanel.
func ListServers(ctx context.Context) ([]Server, error) {
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
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New(resp.Status)
	}
	var servers []Server
	if err := json.NewDecoder(resp.Body).Decode(&servers); err != nil {
		return nil, err
	}
	return servers, nil
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
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New(resp.Status)
	}
	var d ServerDetail
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, err
	}
	return &d, nil
}
