package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	urlpkg "net/url"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	singleflight "golang.org/x/sync/singleflight"
	rate "golang.org/x/time/rate"
	dbpkg "modsentinel/internal/db"
	"modsentinel/internal/httpx"
	mr "modsentinel/internal/modrinth"
	pppkg "modsentinel/internal/pufferpanel"
	"modsentinel/internal/secrets"
	"modsentinel/internal/telemetry"
	tokenpkg "modsentinel/internal/token"
)

type modrinthClient interface {
    Project(ctx context.Context, slug string) (*mr.Project, error)
    Versions(ctx context.Context, slug, gameVersion, loader string) ([]mr.Version, error)
    Resolve(ctx context.Context, slug string) (*mr.Project, string, error)
    Search(ctx context.Context, query string) (*mr.SearchResult, error)
}

var modClient modrinthClient = mr.NewClient()

// allow tests to stub PufferPanel interactions
var (
	ppGetServer = pppkg.GetServer
	ppListPath  = pppkg.ListPath
)

var lastSync atomic.Int64
var latencyP50 atomic.Int64
var latencyP95 atomic.Int64
var resyncAliasHits atomic.Int64 // counts deprecated /resync alias hits
// ALLOW_RESYNC_ALIAS gates the deprecated /resync alias.
// TODO: remove this flag and alias after 2025-01-01.
var allowResyncAlias = func() bool {
	v := strings.ToLower(os.Getenv("ALLOW_RESYNC_ALIAS"))
	return v == "" || v == "1" || v == "true"
}()

var latencyMu sync.Mutex
var latencySamples []int64

var writeLimiter = rate.NewLimiter(rate.Every(time.Second), 5)

var csrfToken string

var (
	listServersTTL   = 2 * time.Second
	listServersSF    singleflight.Group
	listServersCache sync.Map // map[baseURL]listServersEntry
)

type listServersEntry struct {
	servers []pppkg.Server
	exp     time.Time
}

type nonceCtxKey struct{}

func init() {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	csrfToken = base64.StdEncoding.EncodeToString(b)
}

// CSRFToken returns the server CSRF token. Exposed for tests.
func CSRFToken() string { return csrfToken }

func writeModrinthError(w http.ResponseWriter, r *http.Request, err error) {
	var me *mr.Error
	if errors.As(err, &me) && (me.Status == http.StatusUnauthorized || me.Status == http.StatusForbidden) {
		httpx.Write(w, r, httpx.Unauthorized("token required"))
		return
	}
	httpx.Write(w, r, httpx.BadRequest(err.Error()))
}

func writePPError(w http.ResponseWriter, r *http.Request, err error) int {
	var ce *pppkg.ConfigError
	if errors.As(err, &ce) {
		httpx.Write(w, r, httpx.BadRequest(ce.Error()))
		return http.StatusBadRequest
	}
	if errors.Is(err, pppkg.ErrForbidden) {
		httpx.Write(w, r, httpx.Forbidden("insufficient PufferPanel permissions"))
		return http.StatusForbidden
	}
	if errors.Is(err, pppkg.ErrNotFound) {
		http.NotFound(w, r)
		return http.StatusNotFound
	}
	var pe *pppkg.Error
	if errors.As(err, &pe) {
		switch {
		case pe.Status == http.StatusBadRequest:
			httpx.Write(w, r, httpx.BadRequest("bad request to PufferPanel; check base URL"))
			return http.StatusBadRequest
		case pe.Status == http.StatusUnauthorized:
			httpx.Write(w, r, httpx.Unauthorized("invalid PufferPanel credentials"))
			return http.StatusUnauthorized
		case pe.Status == http.StatusForbidden:
			httpx.Write(w, r, httpx.Forbidden("insufficient PufferPanel permissions"))
			return http.StatusForbidden
		case pe.Status >= 500:
			httpx.Write(w, r, httpx.BadGateway(pe.Error()))
			return http.StatusBadGateway
		default:
			httpx.Write(w, r, httpx.BadGateway(pe.Error()))
			return http.StatusBadGateway
		}
	}
	httpx.Write(w, r, httpx.BadGateway(err.Error()))
	return http.StatusBadGateway
}

type metadataRequest struct {
	URL string `json:"url" validate:"required,url"`
}

func validatePayload(v interface{}) *httpx.HTTPError {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	rt := rv.Type()
	fields := map[string]string{}
	for i := 0; i < rv.NumField(); i++ {
		field := rv.Field(i)
		sf := rt.Field(i)
		tag := sf.Tag.Get("validate")
		if tag == "" {
			continue
		}
		name := strings.ToLower(sf.Name)
		if jsonTag := sf.Tag.Get("json"); jsonTag != "" {
			name = strings.Split(jsonTag, ",")[0]
		}
		for _, rule := range strings.Split(tag, ",") {
			switch rule {
			case "required":
				if field.IsZero() {
					fields[name] = "required"
				}
			case "url":
				if _, ok := fields[name]; ok {
					continue
				}
				s, ok := field.Interface().(string)
				if !ok || s == "" {
					fields[name] = "url"
					continue
				}
				if _, err := urlpkg.ParseRequestURI(s); err != nil {
					fields[name] = "url"
				}
			}
		}
	}
	if len(fields) > 0 {
		return httpx.BadRequest("validation failed").WithDetails(fields)
	}
	return nil
}

type instanceReq struct {
    Name                string `json:"name"`
    Loader              string `json:"loader"`
    // Accept both camelCase and snake_case for server id from clients
    ServerID            string `json:"serverId"`
    PufferpanelServerID string `json:"pufferpanel_server_id"`
    EnforceSameLoader   *bool  `json:"enforce_same_loader"`
}

func sanitizeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// validateInstanceReq performs business validations for instance creation.
func validateInstanceReq(ctx context.Context, req *instanceReq) map[string]string {
    req.Name = sanitizeName(req.Name)
    details := map[string]string{}

    // Normalize server id from either field
    serverIDCamel := strings.TrimSpace(req.ServerID)
    serverIDSnake := strings.TrimSpace(req.PufferpanelServerID)
    serverID := serverIDCamel
    if serverID == "" {
        serverID = serverIDSnake
    }

    // Name required only when no server is provided.
    // Preserve stricter behavior for legacy camelCase clients (tests),
    // and relax when snake_case field is used by the frontend.
    requireName := serverIDSnake == ""
    if requireName {
        if req.Name == "" {
            details["name"] = "required"
        } else if len([]rune(req.Name)) > dbpkg.InstanceNameMaxLen {
            details["name"] = "max"
        }
    } else if req.Name != "" && len([]rune(req.Name)) > dbpkg.InstanceNameMaxLen {
        details["name"] = "max"
    }

    // Validate loader if provided; allow empty for server-based creation
    if req.Loader != "" {
        switch strings.ToLower(req.Loader) {
        case "fabric", "forge", "paper", "spigot", "bukkit", "quilt":
        default:
            details["loader"] = "invalid"
        }
    }

    if len(details) > 0 {
        return details
    }

    // Upstream validation only when a server is provided
    if serverID != "" {
        if _, err := ppGetServer(ctx, serverID); err != nil {
            if errors.Is(err, pppkg.ErrNotFound) {
                details["serverId"] = "not_found"
            } else {
                details["upstream"] = "unreachable"
            }
            return details
        }
        folder := "mods"
        switch strings.ToLower(req.Loader) {
        case "paper", "spigot", "bukkit":
            folder = "plugins"
        }
        if _, err := ppListPath(ctx, serverID, folder); err != nil {
            if errors.Is(err, pppkg.ErrNotFound) {
                details["folder"] = "missing"
            } else {
                details["upstream"] = "unreachable"
            }
            return details
        }
    }
    return details
}

func recordLatency(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		dur := time.Since(start).Milliseconds()
		latencyMu.Lock()
		latencySamples = append(latencySamples, dur)
		if len(latencySamples) > 100 {
			latencySamples = latencySamples[1:]
		}
		samples := append([]int64(nil), latencySamples...)
		latencyMu.Unlock()
		if len(samples) == 0 {
			return
		}
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		latencyP50.Store(samples[len(samples)/2])
		idx := (len(samples) * 95) / 100
		if idx >= len(samples) {
			idx = len(samples) - 1
		}
		latencyP95.Store(samples[idx])
	})
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := uuid.NewString()
		ctx := pppkg.WithRequestID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func securityHeaders(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Split style policy for elements vs. attributes to avoid blocking
        // library-provided inline style attributes while keeping <style> tags
        // protected by a nonce in production.
        styleElem := "style-src-elem 'self'"
        styleAttr := "style-src-attr 'unsafe-inline'"
        ctx := r.Context()
        if os.Getenv("APP_ENV") == "production" {
            nonceBytes := make([]byte, 16)
            if _, err := rand.Read(nonceBytes); err == nil {
                nonce := base64.StdEncoding.EncodeToString(nonceBytes)
                styleElem += " 'nonce-" + nonce + "'"
                ctx = context.WithValue(ctx, nonceCtxKey{}, nonce)
            }
        } else {
            // In development, allow inline styles fully for convenience
            styleElem += " 'unsafe-inline'"
        }
        connect := "connect-src 'self'"
        if host := pppkg.APIHost(); host != "" {
            connect += " " + host
        }
        csp := strings.Join([]string{
            "default-src 'self'",
            "frame-ancestors 'none'",
            "base-uri 'none'",
            styleElem,
            styleAttr,
            connect,
            "img-src 'self' data: https:",
        }, "; ")
        w.Header().Set("Content-Security-Policy", csp)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

func csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			http.SetCookie(w, &http.Cookie{Name: "csrf_token", Value: csrfToken, Path: "/", HttpOnly: false, SameSite: http.SameSiteStrictMode})
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie("csrf_token")
		token := r.Header.Get("X-CSRF-Token")
		if err != nil || token == "" || c.Value != token || token != csrfToken {
			httpx.Write(w, r, httpx.Forbidden("invalid csrf token"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requireAdmin() func(http.Handler) http.Handler {
	adminToken := os.Getenv("ADMIN_TOKEN")
	if adminToken == "" {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, "Bearer ") || strings.TrimPrefix(h, "Bearer ") != adminToken {
				httpx.Write(w, r, httpx.Forbidden("admin only"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func requireAuth() func(http.Handler) http.Handler {
	token := os.Getenv("ADMIN_TOKEN")
	if token == "" {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, "Bearer ") || strings.TrimPrefix(h, "Bearer ") != token {
				httpx.Write(w, r, httpx.Unauthorized("token required"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func methodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Allow", http.MethodPost)
	w.WriteHeader(http.StatusMethodNotAllowed)
}

func goneHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusGone)
}

// New builds a router with all HTTP handlers.
func New(db *sql.DB, dist fs.FS, svc *secrets.Service) http.Handler {
    r := chi.NewRouter()

	r.Use(securityHeaders)
	r.Use(recordLatency)
	r.Use(telemetry.HTTP)
	r.Use(requestIDMiddleware)

	r.Get("/favicon.ico", serveFavicon(dist))
	r.Get("/api/instances", listInstancesHandler(db))
	r.Get("/api/instances/{id}", getInstanceHandler(db))
    r.Get("/api/instances/{id:\\d+}/logs", listInstanceLogsHandler(db))
	r.Post("/api/instances/validate", validateInstanceHandler())
	r.Post("/api/instances", createInstanceHandler(db))
	r.Put("/api/instances/{id}", updateInstanceHandler(db))
	r.Delete("/api/instances/{id}", deleteInstanceHandler(db))
	r.With(requireAuth()).Post("/api/instances/sync", listServersHandler(db))
	r.With(requireAuth()).Post("/api/instances/{id:\\d+}/sync", syncHandler(db))
	r.With(requireAuth()).Get("/api/instances/{id:\\d+}/sync", methodNotAllowed)
	r.With(requireAuth()).Get("/api/jobs/{id:\\d+}", jobProgressHandler(db))
	r.With(requireAuth()).Get("/api/jobs/{id:\\d+}/events", jobEventsHandler(db))
	r.With(requireAuth()).Post("/api/jobs/{id:\\d+}/retry", retryFailedHandler(db))
	r.With(requireAuth()).Delete("/api/jobs/{id:\\d+}", cancelJobHandler(db))
	if allowResyncAlias {
		// Temporary alias; TODO: remove after 2025-01-01.
		r.With(requireAuth()).Post("/api/instances/{id:\\d+}/resync", syncHandler(db))
		r.With(requireAuth()).Get("/api/instances/{id:\\d+}/resync", methodNotAllowed)
	} else {
		r.With(requireAuth()).Post("/api/instances/{id:\\d+}/resync", goneHandler)
		r.With(requireAuth()).Get("/api/instances/{id:\\d+}/resync", goneHandler)
	}
    r.Get("/api/mods", listModsHandler(db))
    r.Post("/api/mods/metadata", metadataHandler())
    r.Get("/api/mods/search", searchModsHandler())
    r.Post("/api/mods", createModHandler(db))
	r.Get("/api/mods/{id}/check", checkModHandler(db))
	r.Put("/api/mods/{id}", updateModHandler(db))
	r.Delete("/api/mods/{id}", deleteModHandler(db))
	r.Post("/api/mods/{id}/update", applyUpdateHandler(db))

	r.With(requireAdmin()).Post("/api/pufferpanel/test", testPufferHandler())

	r.Group(func(g chi.Router) {
		g.Use(requireAdmin())
		g.Use(csrfMiddleware)
		g.Post("/api/settings/secret/{type}", setSecretHandler())
		g.Delete("/api/settings/secret/{type}", deleteSecretHandler())
		g.Get("/api/settings/secret/{type}/status", secretStatusHandler(svc))
	})
	r.Get("/api/dashboard", dashboardHandler(db))

    // In development, serve static assets from disk so changes appear without rebuilding Go.
    // Set APP_ENV=development and run `npm run build:watch` in frontend.
    if strings.ToLower(os.Getenv("APP_ENV")) != "production" {
        if disk, err := fs.Sub(os.DirFS("."), "frontend/dist"); err == nil {
            r.Get("/*", serveStatic(disk))
        } else {
            static, _ := fs.Sub(dist, "frontend/dist")
            r.Get("/*", serveStatic(static))
        }
    } else {
        static, _ := fs.Sub(dist, "frontend/dist")
        r.Get("/*", serveStatic(static))
    }

    return r
}

func searchModsHandler() http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        q := strings.TrimSpace(r.URL.Query().Get("q"))
        if q == "" {
            httpx.Write(w, r, httpx.BadRequest("missing query"))
            return
        }
        res, err := modClient.Search(r.Context(), q)
        if err != nil {
            writeModrinthError(w, r, err)
            return
        }
        type hit struct {
            Slug        string `json:"slug"`
            Title       string `json:"title"`
            Description string `json:"description"`
            IconURL     string `json:"icon_url"`
        }
        out := make([]hit, 0, len(res.Hits))
        for _, h := range res.Hits {
            out = append(out, hit{Slug: h.Slug, Title: h.Title, Description: h.Description, IconURL: h.IconURL})
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(out)
    }
}

func serveFavicon(dist fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(dist, "favicon.ico")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/x-icon")
		http.ServeContent(w, r, "favicon.ico", time.Now(), bytes.NewReader(data))
	}
}

func listInstancesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		instances, err := dbpkg.ListInstances(db)
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// Avoid stale list after add/sync flows
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(instances)
	}
}

func getInstanceHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid id"))
			return
		}
		inst, err := dbpkg.GetInstance(db, id)
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// Avoid stale instance stats during/after sync
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(inst)
	}
}

func validateInstanceHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req instanceReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid json"))
			return
		}
		if details := validateInstanceReq(r.Context(), &req); len(details) > 0 {
			telemetry.Event("instance_validation_failed", map[string]string{"correlation_id": uuid.NewString()})
			httpx.Write(w, r, httpx.BadRequest("validation failed").WithDetails(details))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
}

func createInstanceHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var req instanceReq
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            httpx.Write(w, r, httpx.BadRequest("invalid json"))
            return
        }
        if details := validateInstanceReq(r.Context(), &req); len(details) > 0 {
            telemetry.Event("instance_validation_failed", map[string]string{"correlation_id": uuid.NewString()})
            httpx.Write(w, r, httpx.BadRequest("validation failed").WithDetails(details))
            return
        }
        // Normalize incoming fields
        serverIDCamel := strings.TrimSpace(req.ServerID)
        serverIDSnake := strings.TrimSpace(req.PufferpanelServerID)
        serverID := serverIDCamel
        if serverID == "" {
            serverID = serverIDSnake
        }
        enforce := true
        if req.EnforceSameLoader != nil {
            enforce = *req.EnforceSameLoader
        }
        name := req.Name
        // Only auto-derive name when snake_case field is provided by the frontend flow
        if name == "" && serverIDSnake != "" {
            if s, err := ppGetServer(r.Context(), serverIDSnake); err == nil && s != nil {
                name = sanitizeName(s.Name)
                rn := []rune(name)
                if len(rn) > dbpkg.InstanceNameMaxLen {
                    name = string(rn[:dbpkg.InstanceNameMaxLen])
                }
            }
        }
        // Fallback if derived name is still empty
        if strings.TrimSpace(name) == "" {
            base := "Server"
            if serverIDSnake != "" {
                base = fmt.Sprintf("Server %s", serverIDSnake)
            }
            rn := []rune(base)
            if len(rn) > dbpkg.InstanceNameMaxLen {
                base = string(rn[:dbpkg.InstanceNameMaxLen])
            }
            name = base
        }
        inst := dbpkg.Instance{ID: 0, Name: name, Loader: strings.ToLower(req.Loader), PufferpanelServerID: serverID, EnforceSameLoader: enforce}
        tx, err := db.BeginTx(r.Context(), nil)
        if err != nil {
            httpx.Write(w, r, httpx.Internal(err))
            return
        }
		res, err := tx.Exec(`INSERT INTO instances(name, loader, enforce_same_loader, pufferpanel_server_id) VALUES(?,?,?,?)`, inst.Name, inst.Loader, inst.EnforceSameLoader, inst.PufferpanelServerID)
		if err != nil {
			tx.Rollback()
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		id, _ := res.LastInsertId()
		inst.ID = int(id)
		if err := tx.Commit(); err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(inst)
	}
}

func updateInstanceHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid id"))
			return
		}
		inst, err := dbpkg.GetInstance(db, id)
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		var req struct {
			Name              *string `json:"name"`
			Loader            *string `json:"loader"`
			EnforceSameLoader *bool   `json:"enforce_same_loader"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid json"))
			return
		}
		if req.Loader != nil && !strings.EqualFold(*req.Loader, inst.Loader) {
			httpx.Write(w, r, httpx.BadRequest("loader immutable"))
			return
		}
		if req.Name != nil {
			n := sanitizeName(*req.Name)
			if n == "" {
				httpx.Write(w, r, httpx.BadRequest("validation failed").WithDetails(map[string]string{"name": "required"}))
				return
			}
			if len([]rune(n)) > dbpkg.InstanceNameMaxLen {
				httpx.Write(w, r, httpx.BadRequest("validation failed").WithDetails(map[string]string{"name": "max"}))
				return
			}
			inst.Name = n
		}
		if req.EnforceSameLoader != nil {
			inst.EnforceSameLoader = *req.EnforceSameLoader
		}
		if err := validatePayload(inst); err != nil {
			httpx.Write(w, r, err)
			return
		}
		if err := dbpkg.UpdateInstance(db, inst); err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(inst)
	}
}

func deleteInstanceHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid id"))
			return
		}

		var targetID *int
		if tStr := r.URL.Query().Get("target_instance_id"); tStr != "" {
			t, err := strconv.Atoi(tStr)
			if err != nil {
				httpx.Write(w, r, httpx.BadRequest("invalid target_instance_id"))
				return
			}
			targetID = &t
		}

		if err := dbpkg.DeleteInstance(db, id, targetID); err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}

func listModsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.URL.Query().Get("instance_id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid instance_id"))
			return
		}
		mods, err := dbpkg.ListMods(db, id)
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "max-age=60")
		json.NewEncoder(w).Encode(mods)
	}
}

func metadataHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req metadataRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid json"))
			return
		}
		if err := validatePayload(&req); err != nil {
			httpx.Write(w, r, err)
			return
		}
		meta, err := fetchModMetadata(r.Context(), req.URL)
		if err != nil {
			writeModrinthError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(meta)
	}
}

func createModHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Accept core mod fields plus an optional explicit version id chosen in the wizard
        var req struct {
            dbpkg.Mod
            VersionID string `json:"version_id"`
        }
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            httpx.Write(w, r, httpx.BadRequest("invalid json"))
            return
        }
        m := req.Mod
        if err := validatePayload(&m); err != nil {
            httpx.Write(w, r, err)
            return
        }
        inst, err := dbpkg.GetInstance(db, m.InstanceID)
        if err != nil {
            httpx.Write(w, r, httpx.Internal(err))
            return
        }
        warning := ""
        if !strings.EqualFold(inst.Loader, m.Loader) {
            if inst.EnforceSameLoader {
                httpx.Write(w, r, httpx.BadRequest("loader mismatch"))
                return
            }
            warning = "loader mismatch"
        }
        slug, err := parseModrinthSlug(m.URL)
        if err != nil {
            httpx.Write(w, r, httpx.BadRequest(err.Error()))
            return
        }
        if err := populateProjectInfo(r.Context(), &m, slug); err != nil {
            writeModrinthError(w, r, err)
            return
        }
        // If client provided an explicit Modrinth version ID, honor it.
        // Keep the selected file URL separate so later enrichment does not overwrite it.
        selectedURL := ""
        selectedVersion := ""
        if vid := strings.TrimSpace(req.VersionID); vid != "" {
            versions, err := modClient.Versions(r.Context(), slug, m.GameVersion, m.Loader)
            if err != nil {
                writeModrinthError(w, r, err)
                return
            }
            found := false
            for _, v := range versions {
                if v.ID == vid {
                    m.CurrentVersion = v.VersionNumber
                    m.Channel = strings.ToLower(v.VersionType)
                    if len(v.Files) > 0 {
                        m.DownloadURL = v.Files[0].URL
                        selectedURL = m.DownloadURL
                    }
                    selectedVersion = m.CurrentVersion
                    found = true
                    break
                }
            }
            if !found {
                httpx.Write(w, r, httpx.BadRequest("selected version not found"))
                return
            }
            if err := populateAvailableVersion(r.Context(), &m, slug); err != nil {
                writeModrinthError(w, r, err)
                return
            }
        } else {
            if err := populateVersions(r.Context(), &m, slug); err != nil {
                writeModrinthError(w, r, err)
                return
            }
        }
        if err := dbpkg.InsertMod(db, &m); err != nil {
            httpx.Write(w, r, httpx.Internal(err))
            return
        }
        // Log event: mod added (best-effort)
        _ = dbpkg.InsertEvent(db, &dbpkg.ModEvent{InstanceID: m.InstanceID, ModID: &m.ID, Action: "added", ModName: m.Name, To: m.CurrentVersion})
        // If this instance is linked to PufferPanel, attempt to download the selected file
        // and upload it to the appropriate folder on the server (mods/ or plugins/).
        // Use the explicitly selected version file if provided, otherwise fall back to current m.DownloadURL.
        dlURL := m.DownloadURL
        verForName := m.CurrentVersion
        if selectedURL != "" {
            dlURL = selectedURL
            if selectedVersion != "" {
                verForName = selectedVersion
            }
        }
        if inst.PufferpanelServerID != "" && dlURL != "" {
            folder := "mods/"
            switch strings.ToLower(inst.Loader) {
            case "paper", "spigot", "bukkit":
                folder = "plugins/"
            }
            // Derive filename from URL path; fallback to slug-version.jar
            filename := func(raw string) string {
                if u, err := urlpkg.Parse(raw); err == nil {
                    p := u.Path
                    if i := strings.LastIndex(p, "/"); i != -1 && i+1 < len(p) {
                        name := p[i+1:]
                        if name != "" {
                            return name
                        }
                    }
                }
                base := slug
                if base == "" {
                    base = m.Name
                }
                base = strings.TrimSpace(base)
                if base == "" {
                    base = "mod"
                }
                ver := strings.TrimSpace(verForName)
                if ver == "" {
                    ver = "latest"
                }
                return base + "-" + ver + ".jar"
            }(dlURL)
            // Fetch file bytes
            reqDL, err := http.NewRequestWithContext(r.Context(), http.MethodGet, dlURL, nil)
            if err == nil {
                resp, err := http.DefaultClient.Do(reqDL)
                if err == nil {
                    defer resp.Body.Close()
                    if resp.StatusCode >= 200 && resp.StatusCode < 300 {
                        data, _ := io.ReadAll(resp.Body)
                        if len(data) > 0 {
                            if err := pppkg.PutFile(r.Context(), inst.PufferpanelServerID, folder+filename, data); err != nil {
                                // Surface as a non-fatal warning
                                if warning == "" {
                                    warning = "failed to upload file to PufferPanel"
                                }
                            }
                        } else if warning == "" {
                            warning = "failed to download selected file"
                        }
                    } else if warning == "" {
                        warning = "failed to download selected file"
                    }
                } else if warning == "" {
                    warning = "failed to download selected file"
                }
            } else if warning == "" {
                warning = "failed to download selected file"
            }
        }
        mods, err := dbpkg.ListMods(db, m.InstanceID)
        if err != nil {
            httpx.Write(w, r, httpx.Internal(err))
            return
        }
        w.Header().Set("Content-Type", "application/json")
        w.Header().Set("Cache-Control", "no-store")
        json.NewEncoder(w).Encode(struct {
            Mods    []dbpkg.Mod `json:"mods"`
            Warning string      `json:"warning,omitempty"`
        }{mods, warning})
    }
}

func checkModHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid id"))
			return
		}
		m, err := dbpkg.GetMod(db, id)
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		slug, err := parseModrinthSlug(m.URL)
		if err != nil {
			httpx.Write(w, r, httpx.BadRequest(err.Error()))
			return
		}
		if err := populateAvailableVersion(r.Context(), m, slug); err != nil {
			writeModrinthError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(m)
	}
}

func updateModHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        idStr := chi.URLParam(r, "id")
        id, err := strconv.Atoi(idStr)
        if err != nil {
            httpx.Write(w, r, httpx.BadRequest("invalid id"))
            return
        }
        // Load existing mod to detect version/file changes for PufferPanel
        prev, err := dbpkg.GetMod(db, id)
        if err != nil {
            httpx.Write(w, r, httpx.Internal(err))
            return
        }
        var m dbpkg.Mod
        if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
            httpx.Write(w, r, httpx.BadRequest("invalid json"))
            return
        }
		m.ID = id
		if err := validatePayload(&m); err != nil {
			httpx.Write(w, r, err)
			return
		}
		slug, err := parseModrinthSlug(m.URL)
		if err != nil {
			httpx.Write(w, r, httpx.BadRequest(err.Error()))
			return
		}
		if err := populateProjectInfo(r.Context(), &m, slug); err != nil {
			writeModrinthError(w, r, err)
			return
		}
		if err := populateVersions(r.Context(), &m, slug); err != nil {
			writeModrinthError(w, r, err)
			return
		}
        if err := dbpkg.UpdateMod(db, &m); err != nil {
            httpx.Write(w, r, httpx.Internal(err))
            return
        }
        if prev.CurrentVersion != m.CurrentVersion {
            _ = dbpkg.InsertEvent(db, &dbpkg.ModEvent{InstanceID: m.InstanceID, ModID: &m.ID, Action: "updated", ModName: m.Name, From: prev.CurrentVersion, To: m.CurrentVersion})
        }
        // If instance is linked to PufferPanel and the version changed, reflect update on server
        if inst, err2 := dbpkg.GetInstance(db, m.InstanceID); err2 == nil && inst.PufferpanelServerID != "" {
            folder := "mods/"
            switch strings.ToLower(inst.Loader) {
            case "paper", "spigot", "bukkit":
                folder = "plugins/"
            }
            // Helper to derive filename from URL or fallback slug-version.jar
            deriveName := func(rawURL, slug, defName, version string) string {
                if u, err := urlpkg.Parse(rawURL); err == nil {
                    p := u.Path
                    if i := strings.LastIndex(p, "/"); i != -1 && i+1 < len(p) {
                        name := p[i+1:]
                        if name != "" {
                            return name
                        }
                    }
                }
                base := strings.TrimSpace(slug)
                if base == "" { base = strings.TrimSpace(defName) }
                if base == "" { base = "mod" }
                ver := strings.TrimSpace(version)
                if ver == "" { ver = "latest" }
                return base + "-" + ver + ".jar"
            }
            oldSlug, _ := parseModrinthSlug(prev.URL)
            newSlug, _ := parseModrinthSlug(m.URL)
            oldName := deriveName(prev.DownloadURL, oldSlug, prev.Name, prev.CurrentVersion)
            newName := deriveName(m.DownloadURL, newSlug, m.Name, m.CurrentVersion)
            if oldName != newName || prev.CurrentVersion != m.CurrentVersion {
                // Try delete old
                if files, err := pppkg.ListPath(r.Context(), inst.PufferpanelServerID, folder); err == nil {
                    for _, f := range files {
                        if !f.IsDir && strings.EqualFold(f.Name, oldName) {
                            _ = pppkg.DeleteFile(r.Context(), inst.PufferpanelServerID, folder+oldName)
                            break
                        }
                    }
                } else {
                    _ = pppkg.DeleteFile(r.Context(), inst.PufferpanelServerID, folder+oldName)
                }
                // Upload new
                if m.DownloadURL != "" {
                    if reqDL, err := http.NewRequestWithContext(r.Context(), http.MethodGet, m.DownloadURL, nil); err == nil {
                        if resp, err := http.DefaultClient.Do(reqDL); err == nil {
                            defer resp.Body.Close()
                            if resp.StatusCode >= 200 && resp.StatusCode < 300 {
                                if data, err := io.ReadAll(resp.Body); err == nil && len(data) > 0 {
                                    _ = pppkg.PutFile(r.Context(), inst.PufferpanelServerID, folder+newName, data)
                                }
                            }
                        }
                    }
                }
            }
        }
        mods, err := dbpkg.ListMods(db, m.InstanceID)
        if err != nil {
            httpx.Write(w, r, httpx.Internal(err))
            return
        }
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(mods)
	}
}

func deleteModHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        idStr := chi.URLParam(r, "id")
        id, err := strconv.Atoi(idStr)
        if err != nil {
            http.Error(w, "invalid id", http.StatusBadRequest)
            return
        }
        instStr := r.URL.Query().Get("instance_id")
        instID, err := strconv.Atoi(instStr)
        if err != nil {
            http.Error(w, "invalid instance_id", http.StatusBadRequest)
            return
        }
        // Attempt to delete the file from PufferPanel if linked
        var before *dbpkg.Mod
        if mb, err := dbpkg.GetMod(db, id); err == nil { before = mb }
        if m, err := dbpkg.GetMod(db, id); err == nil {
            if inst, err2 := dbpkg.GetInstance(db, m.InstanceID); err2 == nil && inst.PufferpanelServerID != "" {
                folder := "mods/"
                switch strings.ToLower(inst.Loader) {
                case "paper", "spigot", "bukkit":
                    folder = "plugins/"
                }
                slug, _ := parseModrinthSlug(m.URL)
                // Candidate names: URL basename then slug-version.jar
                candidates := []string{}
                if u, err := urlpkg.Parse(m.DownloadURL); err == nil {
                    if p := u.Path; p != "" {
                        if i := strings.LastIndex(p, "/"); i != -1 && i+1 < len(p) {
                            if name := p[i+1:]; name != "" { candidates = append(candidates, name) }
                        }
                    }
                }
                base := strings.TrimSpace(slug)
                if base == "" { base = strings.TrimSpace(m.Name) }
                if base == "" { base = "mod" }
                ver := strings.TrimSpace(m.CurrentVersion)
                if ver == "" { ver = "latest" }
                candidates = append(candidates, base+"-"+ver+".jar")
                if files, err := pppkg.ListPath(r.Context(), inst.PufferpanelServerID, folder); err == nil {
                    present := map[string]bool{}
                    for _, f := range files { present[strings.ToLower(f.Name)] = true }
                    for _, nm := range candidates {
                        if present[strings.ToLower(nm)] { _ = pppkg.DeleteFile(r.Context(), inst.PufferpanelServerID, folder+nm); break }
                    }
                } else {
                    for _, nm := range candidates { _ = pppkg.DeleteFile(r.Context(), inst.PufferpanelServerID, folder+nm) }
                }
            }
        }
        if err := dbpkg.DeleteMod(db, id); err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        if before != nil {
            _ = dbpkg.InsertEvent(db, &dbpkg.ModEvent{InstanceID: before.InstanceID, ModID: &before.ID, Action: "deleted", ModName: before.Name, From: before.CurrentVersion})
        }
        mods, err := dbpkg.ListMods(db, instID)
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(mods)
	}
}

func applyUpdateHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        idStr := chi.URLParam(r, "id")
        id, err := strconv.Atoi(idStr)
        if err != nil {
            httpx.Write(w, r, httpx.BadRequest("invalid id"))
            return
        }
        // Load existing for old version/filename
        prev, err := dbpkg.GetMod(db, id)
        if err != nil {
            httpx.Write(w, r, httpx.Internal(err))
            return
        }
        m, err := dbpkg.ApplyUpdate(db, id)
        if err != nil {
            httpx.Write(w, r, httpx.Internal(err))
            return
        }
        // Ensure m.DownloadURL points to the new version's file for upload
        if slug, err := parseModrinthSlug(m.URL); err == nil {
            if versions, err2 := modClient.Versions(r.Context(), slug, m.GameVersion, m.Loader); err2 == nil {
                for _, vv := range versions {
                    if vv.VersionNumber == m.CurrentVersion {
                        if len(vv.Files) > 0 && vv.Files[0].URL != "" {
                            if m.DownloadURL != vv.Files[0].URL {
                                m.DownloadURL = vv.Files[0].URL
                                _ = dbpkg.UpdateMod(db, m)
                            }
                        }
                        break
                    }
                }
            }
        }
        // Mirror change to PufferPanel if configured
        if inst, err2 := dbpkg.GetInstance(db, m.InstanceID); err2 == nil && inst.PufferpanelServerID != "" {
            folder := "mods/"
            switch strings.ToLower(inst.Loader) {
            case "paper", "spigot", "bukkit":
                folder = "plugins/"
            }
            deriveName := func(rawURL, slug, defName, version string) string {
                if u, err := urlpkg.Parse(rawURL); err == nil {
                    p := u.Path
                    if i := strings.LastIndex(p, "/"); i != -1 && i+1 < len(p) {
                        name := p[i+1:]
                        if name != "" { return name }
                    }
                }
                base := strings.TrimSpace(slug)
                if base == "" { base = strings.TrimSpace(defName) }
                if base == "" { base = "mod" }
                ver := strings.TrimSpace(version)
                if ver == "" { ver = "latest" }
                return base + "-" + ver + ".jar"
            }
            oldSlug, _ := parseModrinthSlug(prev.URL)
            newSlug, _ := parseModrinthSlug(m.URL)
            oldName := deriveName(prev.DownloadURL, oldSlug, prev.Name, prev.CurrentVersion)
            newName := deriveName(m.DownloadURL, newSlug, m.Name, m.CurrentVersion)
            if oldName != newName || prev.CurrentVersion != m.CurrentVersion {
                if files, err := pppkg.ListPath(r.Context(), inst.PufferpanelServerID, folder); err == nil {
                    for _, f := range files {
                        if !f.IsDir && strings.EqualFold(f.Name, oldName) { _ = pppkg.DeleteFile(r.Context(), inst.PufferpanelServerID, folder+oldName); break }
                    }
                } else { _ = pppkg.DeleteFile(r.Context(), inst.PufferpanelServerID, folder+oldName) }
                if m.DownloadURL != "" {
                    if reqDL, err := http.NewRequestWithContext(r.Context(), http.MethodGet, m.DownloadURL, nil); err == nil {
                        if resp, err := http.DefaultClient.Do(reqDL); err == nil {
                            defer resp.Body.Close()
                            if resp.StatusCode >= 200 && resp.StatusCode < 300 {
                                if data, err := io.ReadAll(resp.Body); err == nil && len(data) > 0 {
                                    _ = pppkg.PutFile(r.Context(), inst.PufferpanelServerID, folder+newName, data)
                                }
                            }
                        }
                    }
                }
            }
        }
        // Log event
        _ = dbpkg.InsertEvent(db, &dbpkg.ModEvent{InstanceID: m.InstanceID, ModID: &m.ID, Action: "updated", ModName: m.Name, From: prev.CurrentVersion, To: m.CurrentVersion})
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(m)
    }
}

func listInstanceLogsHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        idStr := chi.URLParam(r, "id")
        id, err := strconv.Atoi(idStr)
        if err != nil {
            httpx.Write(w, r, httpx.BadRequest("invalid id"))
            return
        }
        limit := 100
        if s := strings.TrimSpace(r.URL.Query().Get("limit")); s != "" {
            if n, err := strconv.Atoi(s); err == nil {
                limit = n
            }
        }
        events, err := dbpkg.ListEvents(db, id, limit)
        if err != nil {
            httpx.Write(w, r, httpx.Internal(err))
            return
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(events)
    }
}

type tokenRequest struct {
	Token string `json:"token" validate:"required"`
}

type pufferRequest struct {
	BaseURL      string `json:"base_url" validate:"required,url"`
	ClientID     string `json:"client_id" validate:"required"`
	ClientSecret string `json:"client_secret" validate:"required"`
	Scopes       string `json:"scopes"`
	DeepScan     bool   `json:"deep_scan"`
}

func setSecretHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !writeLimiter.Allow() {
			httpx.Write(w, r, httpx.TooManyRequests("rate limit exceeded"))
			return
		}
		typ := chi.URLParam(r, "type")
		var last4 string
		switch typ {
		case "modrinth":
			var req tokenRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				httpx.Write(w, r, httpx.BadRequest("invalid json"))
				return
			}
			if err := validatePayload(&req); err != nil {
				httpx.Write(w, r, err)
				return
			}
			if n := len(req.Token); n > 4 {
				last4 = req.Token[n-4:]
			} else {
				last4 = req.Token
			}
			if err := tokenpkg.SetToken(req.Token); err != nil {
				httpx.Write(w, r, httpx.Internal(err))
				return
			}
		case "pufferpanel":
			var req pufferRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				httpx.Write(w, r, httpx.BadRequest("invalid json"))
				return
			}
			if err := validatePayload(&req); err != nil {
				httpx.Write(w, r, err)
				return
			}
			if n := len(req.ClientSecret); n > 4 {
				last4 = req.ClientSecret[n-4:]
			} else {
				last4 = req.ClientSecret
			}
			creds := pppkg.Credentials{BaseURL: req.BaseURL, ClientID: req.ClientID, ClientSecret: req.ClientSecret, Scopes: req.Scopes, DeepScan: req.DeepScan}
			if err := pppkg.TestConnection(r.Context(), creds); err != nil {
				writePPError(w, r, err)
				return
			}
			if err := pppkg.Set(creds); err != nil {
				httpx.Write(w, r, httpx.Internal(err))
				return
			}
		default:
			httpx.Write(w, r, httpx.BadRequest("unknown secret type"))
			return
		}
		telemetry.Event("secret_set", map[string]string{"type": typ})
		log.Info().Str("type", typ).Str("last4", last4).Msg("secret set")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}

func deleteSecretHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !writeLimiter.Allow() {
			httpx.Write(w, r, httpx.TooManyRequests("rate limit exceeded"))
			return
		}
		typ := chi.URLParam(r, "type")
		var err error
		switch typ {
		case "modrinth":
			err = tokenpkg.ClearToken()
		case "pufferpanel":
			err = pppkg.Clear()
		default:
			httpx.Write(w, r, httpx.BadRequest("unknown secret type"))
			return
		}
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		telemetry.Event("secret_cleared", map[string]string{"type": typ})
		log.Info().Str("type", typ).Msg("secret deleted")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}

func secretStatusHandler(svc *secrets.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		typ := chi.URLParam(r, "type")
		var (
			exists    bool
			last4     string
			updatedAt time.Time
			err       error
		)
		if typ == "pufferpanel" {
			exists, last4, updatedAt, err = svc.Status(r.Context(), "puffer.oauth_client_secret")
		} else {
			exists, last4, updatedAt, err = svc.Status(r.Context(), typ)
		}
		if err != nil {
			if exists {
				httpx.Write(w, r, httpx.NotFound("secret invalid"))
			} else {
				httpx.Write(w, r, httpx.Internal(err))
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(map[string]any{
			"exists":     exists,
			"last4":      last4,
			"updated_at": updatedAt,
		})
		log.Info().Str("type", typ).Str("last4", last4).Msg("secret status")
	}
}

func testPufferHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		creds, err := pppkg.Get()
		if err != nil {
			writePPError(w, r, err)
			return
		}
		if creds == (pppkg.Credentials{}) {
			httpx.Write(w, r, httpx.BadRequest("credentials not configured"))
			return
		}
		if err := pppkg.TestConnection(r.Context(), creds); err != nil {
			writePPError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func listServersHandler(db *sql.DB) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		status := http.StatusOK
		cacheHit := false
		deduped := false
		upstreamStatus := 0
		defer func() {
			telemetry.Event("instances_sync", map[string]string{
				"status":          strconv.Itoa(status),
				"duration_ms":     strconv.FormatInt(time.Since(start).Milliseconds(), 10),
				"deduped":         strconv.FormatBool(deduped),
				"cache_hit":       strconv.FormatBool(cacheHit),
				"upstream_status": strconv.Itoa(upstreamStatus),
			})
		}()

		creds, err := pppkg.Config()
		if err != nil {
			status = writePPError(w, r, err)
			return
		}
		var servers []pppkg.Server
		if v, ok := listServersCache.Load(creds.BaseURL); ok {
			ent := v.(listServersEntry)
			if time.Now().Before(ent.exp) {
				cacheHit = true
				servers = ent.servers
			}
		}
            if servers == nil {
                var shared bool
                var v any
                v, err, shared = listServersSF.Do(creds.BaseURL, func() (any, error) {
                    svs, us, err := pppkg.ListServersWithStatus(r.Context())
                    upstreamStatus = us
                    if err != nil {
                        return nil, err
                    }
                    return svs, nil
                })
                deduped = shared
                if err != nil {
                    status = writePPError(w, r, err)
                    return
                }
                servers = v.([]pppkg.Server)
                listServersCache.Store(creds.BaseURL, listServersEntry{servers: servers, exp: time.Now().Add(listServersTTL)})
            }
            w.Header().Set("Content-Type", "application/json")
            json.NewEncoder(w).Encode(servers)
        }
}

func syncHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/resync") {
			if !allowResyncAlias {
				w.WriteHeader(http.StatusGone)
				return
			}
			hits := resyncAliasHits.Add(1)
			telemetry.Event("instances_sync_alias", map[string]string{
				"path_alias": "resync",
				"hits":       strconv.FormatInt(hits, 10),
			})
			log.Warn().Str("path", r.URL.Path).Str("path_alias", "resync").Int64("alias_hits", hits).Msg("/api/instances/{id}/resync is deprecated; use /sync instead")
		}
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		inst, err := dbpkg.GetInstance(db, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		var req struct {
			ServerID string `json:"serverId"`
			Key      string `json:"key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			if !errors.Is(err, io.EOF) {
				httpx.Write(w, r, httpx.BadRequest("invalid json"))
				return
			}
		}
		serverID := req.ServerID
		if serverID == "" {
			serverID = inst.PufferpanelServerID
		}
		if serverID == "" {
			httpx.Write(w, r, httpx.BadRequest("validation failed").WithDetails(map[string]string{"serverId": "required"}))
			return
		}
		key := req.Key
		if key == "" {
			key = uuid.NewString()
		}
		jobID, _, err := EnqueueSync(r.Context(), db, inst, serverID, key)
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			ID     int    `json:"id"`
			Status string `json:"status"`
		}{jobID, JobQueued})
		return
	}
}

func jobProgressHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		job, err := dbpkg.GetSyncJob(db, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		var total, processed, succeeded, failed int
		var fails []jobFailure
		if jp, ok := progress.Load(id); ok {
			total, processed, succeeded, failed, fails, _ = jp.(*jobProgress).snapshot()
		}
		inQueue := total - processed
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			ID        int          `json:"id"`
			Status    string       `json:"status"`
			Total     int          `json:"total"`
			Processed int          `json:"processed"`
			Succeeded int          `json:"succeeded"`
			Failed    int          `json:"failed"`
			InQueue   int          `json:"in_queue"`
			Failures  []jobFailure `json:"failures"`
		}{job.ID, job.Status, total, processed, succeeded, failed, inQueue, fails})
	}
}

func jobEventsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if _, err := dbpkg.GetSyncJob(db, id); err != nil {
			http.NotFound(w, r)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "stream unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		p, _ := progress.LoadOrStore(id, newJobProgress())
		jp := p.(*jobProgress)
		ch := jp.subscribe()
		defer jp.unsubscribe(ch)

		send := func() bool {
			total, processed, succeeded, failed, fails, status := jp.snapshot()
			inQueue := total - processed
			data, _ := json.Marshal(struct {
				ID        int          `json:"id"`
				Status    string       `json:"status"`
				Total     int          `json:"total"`
				Processed int          `json:"processed"`
				Succeeded int          `json:"succeeded"`
				Failed    int          `json:"failed"`
				InQueue   int          `json:"in_queue"`
				Failures  []jobFailure `json:"failures"`
			}{id, status, total, processed, succeeded, failed, inQueue, fails})
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return false
			}
			flusher.Flush()
			switch status {
			case JobSucceeded, JobFailed, JobCanceled:
				return false
			}
			return true
		}

		if !send() {
			return
		}
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ch:
				if !send() {
					return
				}
			}
		}
	}
}

func cancelJobHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		job, err := dbpkg.GetSyncJob(db, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		switch job.Status {
		case JobQueued:
			_ = dbpkg.MarkSyncJobFinished(db, id, JobCanceled, "")
			if ch, ok := waiters.Load(id); ok {
				close(ch.(chan struct{}))
				waiters.Delete(id)
			}
		case JobRunning:
			if c, ok := jobCancels.Load(id); ok {
				c.(context.CancelFunc)()
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func retryFailedHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		job, err := dbpkg.GetSyncJob(db, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if job.Status == JobQueued || job.Status == JobRunning {
			http.Error(w, "job active", http.StatusConflict)
			return
		}
		p, ok := progress.Load(id)
		if !ok {
			http.Error(w, "no failures", http.StatusBadRequest)
			return
		}
		_, _, _, _, fails, _ := p.(*jobProgress).snapshot()
		if len(fails) == 0 {
			http.Error(w, "no failures", http.StatusBadRequest)
			return
		}
		names := make([]string, len(fails))
		for i, f := range fails {
			names[i] = f.Name
		}
		if err := dbpkg.RequeueSyncJob(db, id); err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		np := newJobProgress()
		np.setStatus(JobQueued)
		np.setTotal(len(names))
		progress.Store(id, np)
		retryFiles.Store(id, names)
		ch := make(chan struct{})
		waiters.Store(id, ch)
		jobsCh <- id
		recordQueueMetrics()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			ID int `json:"id"`
		}{id})
	}
}

func performSync(ctx context.Context, w http.ResponseWriter, r *http.Request, db *sql.DB, inst *dbpkg.Instance, serverID string, prog *jobProgress, only []string) {
	srv, err := ppGetServer(ctx, serverID)
	if err != nil {
		writePPError(w, r, err)
		return
	}
	inst.Loader = strings.ToLower(srv.Environment.Type)
	inst.PufferpanelServerID = serverID
	inst.EnforceSameLoader = true
	if inst.Loader == "" {
		inst.Loader = "fabric"
	}
	if err := validatePayload(inst); err != nil {
		httpx.Write(w, r, err)
		return
	}
	if _, err := db.Exec(`UPDATE instances SET loader=?, enforce_same_loader=?, pufferpanel_server_id=? WHERE id=?`, inst.Loader, inst.EnforceSameLoader, inst.PufferpanelServerID, inst.ID); err != nil {
		httpx.Write(w, r, httpx.Internal(err))
		return
	}
	creds, err := pppkg.Get()
	if err != nil {
		httpx.Write(w, r, httpx.Internal(err))
		return
	}
	folder := "mods/"
	switch inst.Loader {
	case "paper", "spigot":
		folder = "plugins/"
	}
	var files []string
	if len(only) > 0 {
		files = append([]string(nil), only...)
	} else {
		entries, err := ppListPath(ctx, serverID, folder)
		if err != nil {
			if errors.Is(err, pppkg.ErrNotFound) {
				msg := strings.TrimSuffix(folder, "/") + " folder missing"
				httpx.Write(w, r, httpx.NotFound(msg))
				return
			}
			writePPError(w, r, err)
			return
		}
		files = make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir {
				continue
			}
			if strings.HasSuffix(strings.ToLower(e.Name), ".jar") {
				files = append(files, e.Name)
			}
		}
		sort.Strings(files)
	}
    prog.setTotal(len(files))
    matched := make([]dbpkg.Mod, 0)
    // Track discovered canonical URLs from server to drive deletions
    discovered := make(map[string]struct{})
    // Basic counts
    var addedCount, updatedCount int
    log.Debug().
        Int("instance_id", inst.ID).
        Str("server_id", serverID).
        Str("loader", inst.Loader).
        Int("files_count", len(files)).
        Msg("starting sync scan")

    // Build a quick lookup of existing mods by canonical URL to avoid duplicates
    existingMods, err := dbpkg.ListMods(db, inst.ID)
    if err != nil {
        httpx.Write(w, r, httpx.Internal(err))
        return
    }
    existingByURL := make(map[string]dbpkg.Mod, len(existingMods))
    for _, em := range existingMods {
        existingByURL[strings.TrimSpace(strings.ToLower(em.URL))] = em
    }
	unmatched := make([]string, 0, len(files))

    for _, f := range files {
        if ctx.Err() != nil {
            return
        }
        meta := parseJarFilename(f)
        slug, ver := meta.Slug, meta.Version
        scanned := false
		if creds.DeepScan && (slug == "" || ver == "") {
			scanned = true
			time.Sleep(100 * time.Millisecond)
			data, err := pppkg.FetchFile(ctx, serverID, folder+f)
			if err == nil {
				s2, v2 := parseJarMetadata(data)
				if s2 != "" && v2 != "" {
					slug, ver = s2, v2
				}
			}
		}
               if slug == "" || ver == "" {
                        unmatched = append(unmatched, f)
                        prog.fail(f, errors.New("missing slug or version"))
                        log.Debug().
                            Int("instance_id", inst.ID).
                            Str("server_id", serverID).
                            Str("file", f).
                            Bool("deep_scanned", scanned).
                            Msg("modrinth match failed: missing slug or version")
                        if slug != "" {
                                _ = dbpkg.SetModSyncState(db, inst.ID, slug, ver, JobFailed)
                        }
                        continue
               }
               proj, slug, err := modClient.Resolve(ctx, slug)
               if err != nil && creds.DeepScan && !scanned {
			time.Sleep(100 * time.Millisecond)
			data, err2 := pppkg.FetchFile(ctx, serverID, folder+f)
			if err2 == nil {
				s2, v2 := parseJarMetadata(data)
				if s2 != "" && v2 != "" {
					slug, ver = s2, v2
					proj, slug, err = modClient.Resolve(ctx, slug)
				}
			}
		}
               if err != nil {
                        if ctx.Err() != nil {
                                return
                        }
                        unmatched = append(unmatched, f)
                        prog.fail(f, err)
                        log.Debug().
                            Int("instance_id", inst.ID).
                            Str("server_id", serverID).
                            Str("file", f).
                            Str("slug", slug).
                            Str("version", ver).
                            Err(err).
                            Msg("modrinth resolve failed")
                        _ = dbpkg.SetModSyncState(db, inst.ID, slug, ver, JobFailed)
                        continue
               }
               versions, err := modClient.Versions(ctx, slug, "", "")
               if err != nil {
                        if ctx.Err() != nil {
                                return
                        }
                        unmatched = append(unmatched, f)
                        prog.fail(f, err)
                        log.Debug().
                            Int("instance_id", inst.ID).
                            Str("server_id", serverID).
                            Str("file", f).
                            Str("slug", slug).
                            Str("version", ver).
                            Err(err).
                            Msg("modrinth versions fetch failed")
                        _ = dbpkg.SetModSyncState(db, inst.ID, slug, ver, JobFailed)
                        continue
               }
		var v mr.Version
		found := false
		for _, vv := range versions {
			if vv.VersionNumber == ver {
				v = vv
				found = true
				break
			}
		}
               if !found {
                        unmatched = append(unmatched, f)
                        prog.fail(f, fmt.Errorf("version %s not found", ver))
                        log.Debug().
                            Int("instance_id", inst.ID).
                            Str("server_id", serverID).
                            Str("file", f).
                            Str("slug", slug).
                            Str("version", ver).
                            Msg("modrinth match failed: version not found")
                        _ = dbpkg.SetModSyncState(db, inst.ID, slug, ver, JobFailed)
                        continue
               }
        m := dbpkg.Mod{
            Name:           proj.Title,
            IconURL:        proj.IconURL,
            URL:            fmt.Sprintf("https://modrinth.com/mod/%s", slug),
            InstanceID:     inst.ID,
            Channel:        strings.ToLower(v.VersionType),
            CurrentVersion: v.VersionNumber,
        }
		if len(v.GameVersions) > 0 {
			m.GameVersion = v.GameVersions[0]
		}
		if len(v.Loaders) > 0 {
			m.Loader = v.Loaders[0]
		} else {
			m.Loader = inst.Loader
		}
        if len(v.Files) > 0 {
            m.DownloadURL = v.Files[0].URL
        }
               if err := populateAvailableVersion(ctx, &m, slug); err != nil {
                        if ctx.Err() != nil {
                                return
                        }
                        unmatched = append(unmatched, f)
                        prog.fail(f, err)
                        _ = dbpkg.SetModSyncState(db, inst.ID, slug, ver, JobFailed)
                        continue
               }
               // Deduplicate by canonical URL per instance. Update existing instead of inserting.
               key := strings.TrimSpace(strings.ToLower(m.URL))
               discovered[key] = struct{}{}
               if prev, ok := existingByURL[key]; ok {
                        // Update fields if changed to reflect current scan
                        m.ID = prev.ID
                        if prev.Name != m.Name || prev.IconURL != m.IconURL || prev.GameVersion != m.GameVersion || prev.Loader != m.Loader || prev.Channel != m.Channel || prev.CurrentVersion != m.CurrentVersion || prev.AvailableVersion != m.AvailableVersion || prev.AvailableChannel != m.AvailableChannel || prev.DownloadURL != m.DownloadURL {
                                if err := dbpkg.UpdateMod(db, &m); err != nil {
                                        if ctx.Err() != nil {
                                                return
                                        }
                                        unmatched = append(unmatched, f)
                                        prog.fail(f, err)
                                        _ = dbpkg.SetModSyncState(db, inst.ID, slug, ver, JobFailed)
                                        continue
                                }
                                if prev.CurrentVersion != m.CurrentVersion {
                                    _ = dbpkg.InsertEvent(db, &dbpkg.ModEvent{InstanceID: m.InstanceID, ModID: &m.ID, Action: "updated", ModName: m.Name, From: prev.CurrentVersion, To: m.CurrentVersion})
                                }
                                updatedCount++
                                log.Debug().
                                    Int("instance_id", inst.ID).
                                    Str("server_id", serverID).
                                    Str("file", f).
                                    Str("slug", slug).
                                    Str("name", m.Name).
                                    Str("version", m.CurrentVersion).
                                    Str("loader", m.Loader).
                                    Msg("updated mod for instance")
                        }
                        matched = append(matched, m)
                        prog.success()
                        _ = dbpkg.SetModSyncState(db, inst.ID, slug, ver, JobSucceeded)
                        continue
               }
               if err := dbpkg.InsertMod(db, &m); err != nil {
                        if ctx.Err() != nil {
                                return
                        }
                        unmatched = append(unmatched, f)
                        prog.fail(f, err)
                        _ = dbpkg.SetModSyncState(db, inst.ID, slug, ver, JobFailed)
                        continue
               }
               // Track newly inserted so subsequent duplicates in same run update instead of reinsert
               existingByURL[key] = m
               discovered[key] = struct{}{}
               addedCount++
               _ = dbpkg.InsertEvent(db, &dbpkg.ModEvent{InstanceID: m.InstanceID, ModID: &m.ID, Action: "added", ModName: m.Name, To: m.CurrentVersion})
               log.Debug().
                    Int("instance_id", inst.ID).
                    Str("server_id", serverID).
                    Str("file", f).
                    Str("slug", slug).
                    Str("name", m.Name).
                    Str("version", m.CurrentVersion).
                    Str("loader", m.Loader).
                    Msg("added mod to instance")
               matched = append(matched, m)
               prog.success()
               _ = dbpkg.SetModSyncState(db, inst.ID, slug, ver, JobSucceeded)
    }
    // Build a quick set of existing jar filenames for presence checks
    fileSet := make(map[string]struct{}, len(files))
    for _, name := range files { fileSet[strings.ToLower(name)] = struct{}{} }
    // Delete mods from DB that have no corresponding jar on the server
    for _, em := range existingMods {
        // Candidates: basename of download_url, or slug-currentVersion.jar
        candidates := []string{}
        if u, err := urlpkg.Parse(em.DownloadURL); err == nil {
            if p := u.Path; p != "" {
                if i := strings.LastIndex(p, "/"); i != -1 && i+1 < len(p) {
                    if nm := p[i+1:]; nm != "" { candidates = append(candidates, strings.ToLower(nm)) }
                }
            }
        }
        if slug, err := parseModrinthSlug(em.URL); err == nil {
            base := strings.TrimSpace(slug)
            if base == "" { base = strings.TrimSpace(em.Name) }
            if base == "" { base = "mod" }
            ver := strings.TrimSpace(em.CurrentVersion)
            if ver == "" { ver = "latest" }
            candidates = append(candidates, strings.ToLower(base+"-"+ver+".jar"))
        }
        present := false
        for _, c := range candidates {
            if _, ok := fileSet[c]; ok { present = true; break }
        }
        if !present {
            _ = dbpkg.DeleteMod(db, em.ID)
            _ = dbpkg.InsertEvent(db, &dbpkg.ModEvent{InstanceID: em.InstanceID, ModID: &em.ID, Action: "deleted", ModName: em.Name, From: em.CurrentVersion})
            updatedCount++ // treat deletions as instance changes for sync stats
        }
    }
    if err := dbpkg.UpdateInstanceSync(db, inst.ID, addedCount, updatedCount, len(unmatched)); err != nil {
        httpx.Write(w, r, httpx.Internal(err))
        return
    }
    inst2, err := dbpkg.GetInstance(db, inst.ID)
    if err != nil {
        httpx.Write(w, r, httpx.Internal(err))
        return
    }
    // Return full current mod list for the instance after sync
    currentMods, _ := dbpkg.ListMods(db, inst.ID)
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(struct {
        Instance  dbpkg.Instance `json:"instance"`
        Unmatched []string       `json:"unmatched"`
        Mods      []dbpkg.Mod    `json:"mods"`
    }{*inst2, unmatched, currentMods})
}

func dashboardHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats, err := dbpkg.GetDashboardStats(db)
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		resp := struct {
			Tracked      int               `json:"tracked"`
			UpToDate     int               `json:"up_to_date"`
			Outdated     int               `json:"outdated"`
			OutdatedMods []dbpkg.Mod       `json:"outdated_mods"`
			Recent       []dbpkg.ModUpdate `json:"recent_updates"`
			LastSync     int64             `json:"last_sync"`
			LatencyP50   int64             `json:"latency_p50"`
			LatencyP95   int64             `json:"latency_p95"`
		}{
			Tracked:      stats.Tracked,
			UpToDate:     stats.UpToDate,
			Outdated:     stats.Outdated,
			OutdatedMods: stats.OutdatedMods,
			Recent:       stats.RecentUpdates,
			LastSync:     lastSync.Load(),
			LatencyP50:   latencyP50.Load(),
			LatencyP95:   latencyP95.Load(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func serveStatic(static fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}
		data, err := fs.ReadFile(static, strings.TrimPrefix(path, "/"))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				data, err = fs.ReadFile(static, "index.html")
				if err != nil {
					http.NotFound(w, r)
					return
				}
				path = "/index.html"
			} else {
				http.NotFound(w, r)
				return
			}
		}
		if path == "/index.html" {
			if nonce, ok := r.Context().Value(nonceCtxKey{}).(string); ok && nonce != "" {
				// Expose nonce via meta tag for client-side frameworks if needed
				meta := []byte("<meta name=\"csp-nonce\" content=\"" + nonce + "\">")
				data = bytes.Replace(data, []byte("<head>"), []byte("<head>\n    "+string(meta)), 1)
				// Also add nonce attribute to inline style tags to satisfy style-src-elem
				// Replace <style> and <style ...> without nonce
				s := string(data)
				s = strings.ReplaceAll(s, "<style>", "<style nonce=\""+nonce+"\">")
				s = strings.ReplaceAll(s, "<style ", "<style nonce=\""+nonce+"\" ")
				data = []byte(s)
			}
		}
		http.ServeContent(w, r, path, time.Now(), bytes.NewReader(data))
	}
}

// CheckUpdates refreshes available versions for stored mods.
func CheckUpdates(ctx context.Context, db *sql.DB) {
	mods, err := dbpkg.ListAllMods(db)
	if err != nil {
		log.Error().Err(err).Msg("list mods")
		return
	}
	for _, m := range mods {
		slug, err := parseModrinthSlug(m.URL)
		if err != nil {
			continue
		}
		if err := populateAvailableVersion(ctx, &m, slug); err != nil {
			continue
		}
		_, err = db.Exec(`UPDATE mods SET available_version=?, available_channel=?, download_url=? WHERE id=?`, m.AvailableVersion, m.AvailableChannel, m.DownloadURL, m.ID)
		if err != nil {
			log.Error().Err(err).Msg("update version")
		}
	}
	lastSync.Store(time.Now().Unix())
}

type modMetadata struct {
    GameVersions []string   `json:"game_versions"`
    Loaders      []string   `json:"loaders"`
    Channels     []string   `json:"channels"`
    Versions     []uiVersion `json:"versions"`
}

// uiVersion mirrors mr.Version JSON while adding UI helper flags.
type uiVersion struct {
    ID            string        `json:"id"`
    VersionNumber string        `json:"version_number"`
    VersionType   string        `json:"version_type"`
    DatePublished time.Time     `json:"date_published"`
    GameVersions  []string      `json:"game_versions"`
    Loaders       []string      `json:"loaders"`
    Files         []mr.VersionFile `json:"files"`
    IsNewest      bool          `json:"is_newest"`
    IsPrerelease  bool          `json:"is_prerelease"`
}

func fetchModMetadata(ctx context.Context, rawURL string) (*modMetadata, error) {
	slug, err := parseModrinthSlug(rawURL)
	if err != nil {
		return nil, err
	}
    versions, err := modClient.Versions(ctx, slug, "", "")
    if err != nil {
        return nil, err
    }
    meta := &modMetadata{}
    // Determine newest by DatePublished
    var newestIdx int = -1
    for i, v := range versions {
        if newestIdx == -1 || v.DatePublished.After(versions[newestIdx].DatePublished) {
            newestIdx = i
        }
    }
    gvSet := map[string]struct{}{}
    ldSet := map[string]struct{}{}
    chSet := map[string]struct{}{}
    for i, v := range versions {
        // Fill list helpers
        for _, gv := range v.GameVersions {
            gvSet[gv] = struct{}{}
        }
        for _, ld := range v.Loaders {
            ldSet[ld] = struct{}{}
        }
        chSet[strings.ToLower(v.VersionType)] = struct{}{}
        // Add UI-annotated version
        meta.Versions = append(meta.Versions, uiVersion{
            ID:            v.ID,
            VersionNumber: v.VersionNumber,
            VersionType:   v.VersionType,
            DatePublished: v.DatePublished,
            GameVersions:  append([]string(nil), v.GameVersions...),
            Loaders:       append([]string(nil), v.Loaders...),
            Files:         append([]mr.VersionFile(nil), v.Files...),
            IsNewest:      i == newestIdx,
            IsPrerelease:  strings.ToLower(v.VersionType) != "release",
        })
    }
	for gv := range gvSet {
		meta.GameVersions = append(meta.GameVersions, gv)
	}
	for ld := range ldSet {
		meta.Loaders = append(meta.Loaders, ld)
	}
	for ch := range chSet {
		meta.Channels = append(meta.Channels, ch)
	}
	sort.Strings(meta.GameVersions)
	sort.Strings(meta.Loaders)
	sort.Strings(meta.Channels)
	return meta, nil
}

func populateProjectInfo(ctx context.Context, m *dbpkg.Mod, slug string) error {
	info, err := modClient.Project(ctx, slug)
	if err != nil {
		return err
	}
	m.Name = info.Title
	m.IconURL = info.IconURL
	return nil
}

func populateVersions(ctx context.Context, m *dbpkg.Mod, slug string) error {
	versions, err := modClient.Versions(ctx, slug, m.GameVersion, m.Loader)
	if err != nil {
		return err
	}
	var current mr.Version
	found := false
	for _, v := range versions {
		if strings.EqualFold(v.VersionType, m.Channel) {
			current = v
			found = true
			break
		}
	}
	if !found {
		return errors.New("no version found for channel")
	}
	m.CurrentVersion = current.VersionNumber
	if len(current.Files) > 0 {
		m.DownloadURL = current.Files[0].URL
	}
	if err := populateAvailableVersion(ctx, m, slug); err != nil {
		return err
	}
	return nil
}

func populateAvailableVersion(ctx context.Context, m *dbpkg.Mod, slug string) error {
	versions, err := modClient.Versions(ctx, slug, m.GameVersion, m.Loader)
	if err != nil {
		return err
	}
	order := []string{"release", "beta", "alpha"}
	idx := map[string]int{"release": 0, "beta": 1, "alpha": 2}
	start := idx[strings.ToLower(m.Channel)]
	for i := 0; i <= start; i++ {
		ch := order[i]
		for _, v := range versions {
			if strings.EqualFold(v.VersionType, ch) {
				m.AvailableVersion = v.VersionNumber
				m.AvailableChannel = ch
				if len(v.Files) > 0 {
					m.DownloadURL = v.Files[0].URL
				}
				return nil
			}
		}
	}
	m.AvailableVersion = m.CurrentVersion
	m.AvailableChannel = m.Channel
	return nil
}

func parseModrinthSlug(raw string) (string, error) {
	u, err := urlpkg.Parse(raw)
	if err != nil {
		return "", err
	}
	parts := strings.Split(u.Path, "/")
	for i, p := range parts {
		if p == "mod" || p == "plugin" || p == "datapack" || p == "resourcepack" {
			if i+1 < len(parts) {
				return parts[i+1], nil
			}
		}
	}
	return "", errors.New("slug not found")
}

type jarMeta struct {
	Slug      string
	ID        string
	Version   string
	MCVersion string
	Loader    string
	Channel   string
}

func parseJarFilename(name string) jarMeta {
	var meta jarMeta
	name = strings.TrimSuffix(strings.ToLower(name), ".jar")
	rep := strings.NewReplacer("[", "", "]", "", "(", "", ")", "", "{", "", "}", "", "#", "")
	name = rep.Replace(name)
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_' || r == '+'
	})
	if len(parts) == 0 {
		return meta
	}
	semver := regexp.MustCompile(`^v?\d+(?:\.\d+){1,3}[^a-zA-Z]*$`)
	mcver := regexp.MustCompile(`^1\.\d+(?:\.\d+)?$`)
	loaders := map[string]struct{}{"fabric": {}, "forge": {}, "quilt": {}, "neoforge": {}}
	channels := map[string]struct{}{"beta": {}, "alpha": {}, "rc": {}}

	type sv struct {
		idx int
		val string
	}
	semvers := []sv{}
	for i, p := range parts {
		if strings.HasPrefix(p, "mc") {
			v := strings.TrimPrefix(p, "mc")
			if mcver.MatchString(v) && meta.MCVersion == "" {
				meta.MCVersion = v
				continue
			}
		}
		if semver.MatchString(p) {
			semvers = append(semvers, sv{i, strings.TrimPrefix(p, "v")})
			continue
		}
		if _, ok := loaders[p]; ok {
			meta.Loader = p
			continue
		}
		if _, ok := channels[p]; ok {
			meta.Channel = p
			continue
		}
	}
	verIdx := -1
	if len(semvers) > 0 {
		last := semvers[len(semvers)-1]
		verIdx = last.idx
		meta.Version = last.val
		if len(semvers) > 1 {
			prev := semvers[len(semvers)-2]
			if mcver.MatchString(last.val) && !mcver.MatchString(prev.val) {
				meta.Version = prev.val
				verIdx = prev.idx
				meta.MCVersion = last.val
			} else if meta.MCVersion == "" {
				for _, sv := range semvers[:len(semvers)-1] {
					if mcver.MatchString(sv.val) {
						meta.MCVersion = sv.val
						break
					}
				}
			}
		}
	}

	for i, p := range parts {
		if verIdx != -1 && i >= verIdx {
			break
		}
		if _, ok := loaders[p]; ok && i > 0 {
			continue
		}
		if strings.HasPrefix(p, "mc") {
			v := strings.TrimPrefix(p, "mc")
			if mcver.MatchString(v) {
				continue
			}
		}
		if mcver.MatchString(p) {
			continue
		}
		if _, ok := channels[p]; ok && i > 0 {
			continue
		}
		meta.Slug += p + "-"
	}
	meta.Slug = strings.Trim(meta.Slug, "-")
	if meta.Slug != "" {
		parts := strings.Split(meta.Slug, "-")
		if len(parts) > 0 {
			meta.ID = parts[0]
		}
	}
	return meta
}

func parseJarMetadata(data []byte) (slug, version string) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", ""
	}
	for _, f := range zr.File {
		if f.Name == "fabric.mod.json" || f.Name == "quilt.mod.json" {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			var meta struct {
				ID      string `json:"id"`
				Version string `json:"version"`
			}
			if err := json.NewDecoder(rc).Decode(&meta); err == nil {
				slug = meta.ID
				version = meta.Version
			}
			rc.Close()
			return slug, version
		}
	}
	return "", ""
}
