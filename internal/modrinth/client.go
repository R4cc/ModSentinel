package modrinth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	urlpkg "net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/sync/singleflight"

	"modsentinel/internal/telemetry"
	tokenpkg "modsentinel/internal/token"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

const userAgent = "ModSentinel/1.0 (+https://github.com/nl2109/ModSentinel)"

// Client wraps HTTP access to the Modrinth API.
type Client struct {
	http    *http.Client
	sf      singleflight.Group
	ttl     time.Duration
	cache   map[string]cacheEntry
	mu      sync.Mutex
	backoff time.Duration
}

type cacheEntry struct {
	data []byte
	exp  time.Time
}

// NewClient returns a Client with sane defaults.
func NewClient() *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext
	transport.TLSHandshakeTimeout = 5 * time.Second
	transport.ResponseHeaderTimeout = 10 * time.Second
	transport.ExpectContinueTimeout = 1 * time.Second
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 10
	transport.MaxConnsPerHost = 10
	transport.IdleConnTimeout = 90 * time.Second

	return &Client{
		http:  &http.Client{Timeout: 30 * time.Second, Transport: transport},
		ttl:   5 * time.Minute,
		cache: make(map[string]cacheEntry),
	}
}

// Error represents a normalized Modrinth API error.
// Kind categorizes Modrinth errors.
type Kind string

const (
	KindTimeout     Kind = "timeout"
	KindCanceled    Kind = "canceled"
	KindRateLimited Kind = "rate_limited"
	KindServer      Kind = "server_error"
	KindClient      Kind = "client_error"
)

// Error represents a normalized Modrinth API error.
type Error struct {
	Kind    Kind
	Status  int
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "modrinth error"
}

func (e *Error) Unwrap() error { return e.Err }

// randDuration returns a random duration between 0 and max.
// It is declared as a variable to allow tests to stub out randomness.
var randDuration = func(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(max)))
}

// sleep is declared as a variable so tests can stub out actual sleeping.
var sleep = time.Sleep

func redactURL(u *urlpkg.URL) string {
	cpy := *u
	q := cpy.Query()
	for _, k := range []string{"token", "key", "api_key"} {
		if q.Has(k) {
			q.Set(k, "REDACTED")
		}
	}
	cpy.RawQuery = q.Encode()
	return cpy.Redacted()
}

// do executes the request with retry/backoff and decodes JSON into v.
func (c *Client) do(req *http.Request, v interface{}) error {
	key := req.Method + " " + req.URL.String()
	if c.ttl > 0 {
		c.mu.Lock()
		if e, ok := c.cache[key]; ok {
			if time.Now().Before(e.exp) {
				data := e.data
				c.mu.Unlock()
				if v != nil {
					if err := json.Unmarshal(data, v); err != nil {
						return err
					}
				}
				return nil
			}
			delete(c.cache, key)
		}
		c.mu.Unlock()
	}
	data, err, _ := c.sf.Do(key, func() (interface{}, error) {
		c.mu.Lock()
		bo := c.backoff
		c.mu.Unlock()
		if bo > 0 {
			sleep(bo + randDuration(bo))
		}
		tok, _ := tokenpkg.GetToken()
		if tok != "" {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tok))
		}
		req.Header.Set("User-Agent", userAgent)
		var resp *http.Response
		var err error
		var dur time.Duration
		urlStr := redactURL(req.URL)
		for i := 0; i < 3; i++ {
			start := time.Now()
			resp, err = c.http.Do(req)
			dur = time.Since(start)
			attempt := strconv.Itoa(i + 1)
			if err != nil {
				telemetry.Event("modrinth_request", map[string]string{
					"method":      req.Method,
					"url":         urlStr,
					"status":      "error",
					"duration_ms": strconv.FormatInt(dur.Milliseconds(), 10),
					"attempt":     attempt,
				})
				telemetry.Event("modrinth_error", map[string]string{"error": err.Error()})
				kind := KindClient
				switch {
				case errors.Is(err, context.Canceled):
					kind = KindCanceled
				case errors.Is(err, context.DeadlineExceeded):
					kind = KindTimeout
				case func() bool {
					ne, ok := err.(net.Error)
					return ok && ne.Timeout()
				}():
					kind = KindTimeout
				}
				telemetry.Event("modrinth_result", map[string]string{
					"outcome":     "error",
					"kind":        string(kind),
					"duration_ms": strconv.FormatInt(dur.Milliseconds(), 10),
				})
				return nil, &Error{Kind: kind, Err: err}
			}
			telemetry.Event("modrinth_request", map[string]string{
				"method":      req.Method,
				"url":         urlStr,
				"status":      strconv.Itoa(resp.StatusCode),
				"duration_ms": strconv.FormatInt(dur.Milliseconds(), 10),
				"attempt":     attempt,
			})
			if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
				base := 250 * time.Millisecond
				delay := time.Duration(1<<i) * base
				if ra := resp.Header.Get("Retry-After"); ra != "" {
					if secs, err := strconv.Atoi(ra); err == nil {
						raDelay := time.Duration(secs) * time.Second
						if raDelay > delay {
							delay = raDelay
						}
					} else if t, err := http.ParseTime(ra); err == nil {
						raDelay := time.Until(t)
						if raDelay > delay {
							delay = raDelay
						}
					}
				}
				j := randDuration(delay)
				resp.Body.Close()
				sleep(delay + j)
				continue
			}
			break
		}
		if resp == nil {
			telemetry.Event("modrinth_result", map[string]string{
				"outcome":     "error",
				"kind":        string(KindServer),
				"duration_ms": strconv.FormatInt(dur.Milliseconds(), 10),
			})
			return nil, &Error{Kind: KindServer, Message: "no response"}
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			telemetry.Event("modrinth_error", map[string]string{"status": strconv.Itoa(resp.StatusCode)})
			kind := KindClient
			if resp.StatusCode == http.StatusTooManyRequests {
				kind = KindRateLimited
				c.mu.Lock()
				if c.backoff == 0 {
					c.backoff = time.Second
				} else {
					c.backoff *= 2
					if c.backoff > time.Minute {
						c.backoff = time.Minute
					}
				}
				c.mu.Unlock()
			} else {
				if resp.StatusCode >= 500 {
					kind = KindServer
				}
				c.mu.Lock()
				c.backoff = 0
				c.mu.Unlock()
			}
			telemetry.Event("modrinth_result", map[string]string{
				"outcome":     "error",
				"kind":        string(kind),
				"status":      strconv.Itoa(resp.StatusCode),
				"duration_ms": strconv.FormatInt(dur.Milliseconds(), 10),
			})
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
					return nil, &Error{Kind: kind, Status: resp.StatusCode, Message: msg}
				}
			}
			return nil, &Error{Kind: kind, Status: resp.StatusCode, Message: resp.Status}
		}
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			telemetry.Event("modrinth_result", map[string]string{
				"outcome":     "error",
				"kind":        string(KindClient),
				"duration_ms": strconv.FormatInt(dur.Milliseconds(), 10),
			})
			return nil, err
		}
		if c.ttl > 0 {
			c.mu.Lock()
			if c.cache == nil {
				c.cache = make(map[string]cacheEntry)
			}
			c.cache[key] = cacheEntry{data: b, exp: time.Now().Add(c.ttl)}
			c.mu.Unlock()
		}
		telemetry.Event("modrinth_result", map[string]string{
			"outcome":     "success",
			"status":      strconv.Itoa(resp.StatusCode),
			"duration_ms": strconv.FormatInt(dur.Milliseconds(), 10),
		})
		c.mu.Lock()
		c.backoff = 0
		c.mu.Unlock()
		return b, nil
	})
	if err != nil {
		return err
	}
	if v != nil {
		if err := json.Unmarshal(data.([]byte), v); err != nil {
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
        Description string `json:"description"`
        IconURL     string `json:"icon_url"`
    } `json:"hits"`
}

// normalizeQuery trims whitespace, lowercases, and strips common version suffixes
// like "-1.2.3" from the provided query.
func normalizeQuery(q string) string {
	q = strings.TrimSpace(strings.ToLower(q))
	re := regexp.MustCompile(`[-_]?v?\d+(?:\.\d+){1,3}.*$`)
	q = re.ReplaceAllString(q, "")
	q = strings.Trim(q, "-_")
	return q
}

// validateQuery ensures the normalized query is non-empty and free of
// non-ASCII control characters.
func validateQuery(q string) error {
	if q == "" {
		return errors.New("empty query")
	}
	for _, r := range q {
		if unicode.IsControl(r) && r > unicode.MaxASCII {
			return fmt.Errorf("invalid control character %U", r)
		}
	}
	return nil
}

// Search performs a project search.
func (c *Client) Search(ctx context.Context, query string) (*SearchResult, error) {
	query = normalizeQuery(query)
	if err := validateQuery(query); err != nil {
		return nil, err
	}
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

// Resolve fetches a project by slug, falling back to search when the slug is not found.
func (c *Client) Resolve(ctx context.Context, slug string) (*Project, string, error) {
	proj, err := c.Project(ctx, slug)
	if err == nil {
		return proj, slug, nil
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		return nil, "", err
	}
	res, err := c.Search(ctx, slug)
	if err != nil {
		return nil, "", err
	}
	if len(res.Hits) == 0 {
		return nil, "", &Error{Status: http.StatusNotFound, Message: "project not found"}
	}
	slug = res.Hits[0].Slug
	proj, err = c.Project(ctx, slug)
	if err != nil {
		return nil, "", err
	}
	return proj, slug, nil
}
