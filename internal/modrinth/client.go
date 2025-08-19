package modrinth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	urlpkg "net/url"
	"strconv"
	"time"

	"modsentinel/internal/telemetry"
	tokenpkg "modsentinel/internal/token"
)

// Client wraps HTTP access to the Modrinth API.
type Client struct {
	http *http.Client
}

// NewClient returns a Client with sane defaults.
func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 10 * time.Second}}
}

// Error represents a normalized Modrinth API error.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

// do executes the request with retry/backoff and decodes JSON into v.
func (c *Client) do(req *http.Request, v interface{}) error {
	tok, _ := tokenpkg.GetToken()
	if tok != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tok))
	}
	req.Header.Set("User-Agent", "ModSentinel/1.0")
	var resp *http.Response
	var err error
	for i := 0; i < 3; i++ {
		resp, err = c.http.Do(req)
		if err != nil {
			telemetry.Event("modrinth_error", map[string]string{"error": err.Error()})
			return err
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			// retry with exponential backoff
			delay := time.Duration(1<<i) * 250 * time.Millisecond
			resp.Body.Close()
			time.Sleep(delay)
			continue
		}
		break
	}
	if resp == nil {
		return errors.New("no response")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		telemetry.Event("modrinth_error", map[string]string{"status": strconv.Itoa(resp.StatusCode)})
		var apiErr struct {
			Error       string `json:"error"`
			Description string `json:"description"`
		}
		b, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(b, &apiErr); err == nil {
			msg := apiErr.Description
			if msg == "" {
				msg = apiErr.Error
			}
			if msg != "" {
				return &Error{Status: resp.StatusCode, Message: msg}
			}
		}
		return &Error{Status: resp.StatusCode, Message: resp.Status}
	}
	if v != nil {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			return err
		}
	}
	return nil
}

// Project represents a Modrinth project.
type Project struct {
	Title   string `json:"title"`
	IconURL string `json:"icon_url"`
}

// Project fetches project information by slug.
func (c *Client) Project(ctx context.Context, slug string) (*Project, error) {
	url := fmt.Sprintf("https://api.modrinth.com/v2/project/%s", slug)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	var p Project
	if err := c.do(req, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// Version represents a Modrinth project version.
type Version struct {
	ID            string        `json:"id"`
	VersionNumber string        `json:"version_number"`
	VersionType   string        `json:"version_type"`
	DatePublished time.Time     `json:"date_published"`
	GameVersions  []string      `json:"game_versions"`
	Loaders       []string      `json:"loaders"`
	Files         []VersionFile `json:"files"`
}

type VersionFile struct {
	URL string `json:"url"`
}

// Versions fetches versions for a project filtered by game version and loader.
func (c *Client) Versions(ctx context.Context, slug, gameVersion, loader string) ([]Version, error) {
	params := urlpkg.Values{}
	if gameVersion != "" {
		params.Set("game_versions", fmt.Sprintf("[\"%s\"]", gameVersion))
	}
	if loader != "" {
		params.Set("loaders", fmt.Sprintf("[\"%s\"]", loader))
	}
	url := fmt.Sprintf("https://api.modrinth.com/v2/project/%s/version", slug)
	if len(params) > 0 {
		url = url + "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	var v []Version
	if err := c.do(req, &v); err != nil {
		return nil, err
	}
	return v, nil
}

// SearchResult represents a Modrinth search response.
type SearchResult struct {
	Hits []struct {
		ProjectID string `json:"project_id"`
		Slug      string `json:"slug"`
		Title     string `json:"title"`
	} `json:"hits"`
}

// Search performs a project search.
func (c *Client) Search(ctx context.Context, query string) (*SearchResult, error) {
	url := fmt.Sprintf("https://api.modrinth.com/v2/search?query=%s", urlpkg.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	var res SearchResult
	if err := c.do(req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}
