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
	"io/fs"
	"net/http"
	urlpkg "net/url"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"sync"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"fmt"

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
	Name     string `json:"name"`
	Loader   string `json:"loader"`
	ServerID string `json:"serverId"`
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
	if req.Name == "" {
		details["name"] = "required"
	} else if len([]rune(req.Name)) > dbpkg.InstanceNameMaxLen {
		details["name"] = "max"
	}
	switch strings.ToLower(req.Loader) {
	case "fabric", "forge", "paper", "spigot", "bukkit":
	default:
		details["loader"] = "invalid"
	}
	if strings.TrimSpace(req.ServerID) == "" {
		details["serverId"] = "required"
	}
	if len(details) > 0 {
		return details
	}
	// upstream validation
	if _, err := ppGetServer(ctx, req.ServerID); err != nil {
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
	if _, err := ppListPath(ctx, req.ServerID, folder); err != nil {
		if errors.Is(err, pppkg.ErrNotFound) {
			details["folder"] = "missing"
		} else {
			details["upstream"] = "unreachable"
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
		style := "style-src 'self'"
		ctx := r.Context()
		if os.Getenv("APP_ENV") == "production" {
			nonceBytes := make([]byte, 16)
			if _, err := rand.Read(nonceBytes); err == nil {
				nonce := base64.StdEncoding.EncodeToString(nonceBytes)
				style += " 'nonce-" + nonce + "'"
				ctx = context.WithValue(ctx, nonceCtxKey{}, nonce)
			}
		} else {
			style += " 'unsafe-inline'"
		}
		connect := "connect-src 'self'"
		if host := pppkg.APIHost(); host != "" {
			connect += " " + host
		}
		csp := strings.Join([]string{
			"default-src 'self'",
			"frame-ancestors 'none'",
			"base-uri 'none'",
			style,
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
	r.Post("/api/instances/validate", validateInstanceHandler())
	r.Post("/api/instances", createInstanceHandler(db))
	r.Put("/api/instances/{id}", updateInstanceHandler(db))
	r.Delete("/api/instances/{id}", deleteInstanceHandler(db))
	r.With(requireAuth()).Post("/api/instances/sync", listServersHandler(db))
	r.With(requireAuth()).Post("/api/instances/{id:\\d+}/sync", syncHandler(db))
	r.With(requireAuth()).Get("/api/instances/{id:\\d+}/sync", methodNotAllowed)
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

	static, _ := fs.Sub(dist, "frontend/dist")
	r.Get("/*", serveStatic(static))

	return r
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
		w.Header().Set("Cache-Control", "max-age=60")
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
		w.Header().Set("Cache-Control", "max-age=60")
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
		inst := dbpkg.Instance{ID: 0, Name: req.Name, Loader: strings.ToLower(req.Loader), PufferpanelServerID: req.ServerID, EnforceSameLoader: true}
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
		var m dbpkg.Mod
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid json"))
			return
		}
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
		if err := populateVersions(r.Context(), &m, slug); err != nil {
			writeModrinthError(w, r, err)
			return
		}
		if err := dbpkg.InsertMod(db, &m); err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
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
		if err := dbpkg.DeleteMod(db, id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
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
		m, err := dbpkg.ApplyUpdate(db, id)
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(m)
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
		for _, s := range servers {
			var id int
			err := db.QueryRow(`SELECT id FROM instances WHERE pufferpanel_server_id=?`, s.ID).Scan(&id)
			if err == sql.ErrNoRows {
				name := sanitizeName(s.Name)
				rn := []rune(name)
				if len(rn) > dbpkg.InstanceNameMaxLen {
					log.Warn().Str("server_id", s.ID).Msg("pufferpanel server name truncated")
					name = string(rn[:dbpkg.InstanceNameMaxLen])
				}
				inst := dbpkg.Instance{Name: name, EnforceSameLoader: true, PufferpanelServerID: s.ID}
				if err := validatePayload(&inst); err != nil {
					status = http.StatusBadRequest
					httpx.Write(w, r, err)
					return
				}
				if err := dbpkg.InsertInstance(db, &inst); err != nil {
					status = http.StatusInternalServerError
					httpx.Write(w, r, httpx.Internal(err))
					return
				}
			} else if err != nil {
				status = http.StatusInternalServerError
				httpx.Write(w, r, httpx.Internal(err))
				return
			}
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
		var req struct {
			ServerID string `json:"serverId" validate:"required"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid json"))
			return
		}
		if err := validatePayload(&req); err != nil {
			httpx.Write(w, r, err)
			return
		}
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		srv, err := pppkg.GetServer(r.Context(), req.ServerID)
		if err != nil {
			writePPError(w, r, err)
			return
		}
		inst, err := dbpkg.GetInstance(db, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		inst.Loader = strings.ToLower(srv.Environment.Type)
		inst.PufferpanelServerID = req.ServerID
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
		entries, err := pppkg.ListPath(r.Context(), req.ServerID, folder)
		if err != nil {
			if errors.Is(err, pppkg.ErrNotFound) {
				msg := strings.TrimSuffix(folder, "/") + " folder missing"
				httpx.Write(w, r, httpx.NotFound(msg))
				return
			}
			writePPError(w, r, err)
			return
		}
		files := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir {
				continue
			}
			if strings.HasSuffix(strings.ToLower(e.Name), ".jar") {
				files = append(files, e.Name)
			}
		}
		sort.Strings(files)
		matched := make([]dbpkg.Mod, 0)
		unmatched := make([]string, 0, len(files))
		for _, f := range files {
			meta := parseJarFilename(f)
			slug, ver := meta.Slug, meta.Version
			scanned := false
			if creds.DeepScan && (slug == "" || ver == "") {
				scanned = true
				time.Sleep(100 * time.Millisecond)
				data, err := pppkg.FetchFile(r.Context(), req.ServerID, folder+f)
				if err == nil {
					s2, v2 := parseJarMetadata(data)
					if s2 != "" && v2 != "" {
						slug, ver = s2, v2
					}
				}
			}
			if slug == "" || ver == "" {
				unmatched = append(unmatched, f)
				continue
			}
			res, err := modClient.Search(r.Context(), slug)
			if (err != nil || len(res.Hits) == 0) && creds.DeepScan && !scanned {
				time.Sleep(100 * time.Millisecond)
				data, err := pppkg.FetchFile(r.Context(), req.ServerID, folder+f)
				if err == nil {
					s2, v2 := parseJarMetadata(data)
					if s2 != "" && v2 != "" {
						slug, ver = s2, v2
						res, err = modClient.Search(r.Context(), slug)
					}
				}
			}
			if err != nil || len(res.Hits) == 0 {
				unmatched = append(unmatched, f)
				continue
			}
			slug = res.Hits[0].Slug
			proj, err := modClient.Project(r.Context(), slug)
			if err != nil {
				unmatched = append(unmatched, f)
				continue
			}
			versions, err := modClient.Versions(r.Context(), slug, "", "")
			if err != nil {
				unmatched = append(unmatched, f)
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
			if err := populateAvailableVersion(r.Context(), &m, slug); err != nil {
				unmatched = append(unmatched, f)
				continue
			}
			if err := dbpkg.InsertMod(db, &m); err != nil {
				unmatched = append(unmatched, f)
				continue
			}
			matched = append(matched, m)
		}
		if err := dbpkg.UpdateInstanceSync(db, inst.ID, len(matched), 0, len(unmatched)); err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		inst2, err := dbpkg.GetInstance(db, inst.ID)
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Instance  dbpkg.Instance `json:"instance"`
			Unmatched []string       `json:"unmatched"`
			Mods      []dbpkg.Mod    `json:"mods"`
		}{*inst2, unmatched, matched})
	}
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
				meta := []byte("<meta name=\"csp-nonce\" content=\"" + nonce + "\">")
				data = bytes.Replace(data, []byte("<head>"), []byte("<head>\n    "+string(meta)), 1)
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
	GameVersions []string     `json:"game_versions"`
	Loaders      []string     `json:"loaders"`
	Channels     []string     `json:"channels"`
	Versions     []mr.Version `json:"versions"`
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
	gvSet := map[string]struct{}{}
	ldSet := map[string]struct{}{}
	chSet := map[string]struct{}{}
	for _, v := range versions {
		meta.Versions = append(meta.Versions, v)
		for _, gv := range v.GameVersions {
			gvSet[gv] = struct{}{}
		}
		for _, ld := range v.Loaders {
			ldSet[ld] = struct{}{}
		}
		chSet[strings.ToLower(v.VersionType)] = struct{}{}
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
