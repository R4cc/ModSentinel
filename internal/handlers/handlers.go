package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	urlpkg "net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"sync"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/rs/zerolog/log"

	"fmt"

	rate "golang.org/x/time/rate"
	dbpkg "modsentinel/internal/db"
	"modsentinel/internal/httpx"
	mr "modsentinel/internal/modrinth"
	pppkg "modsentinel/internal/pufferpanel"
	"modsentinel/internal/secrets"
	"modsentinel/internal/telemetry"
	tokenpkg "modsentinel/internal/token"
)

var validate = validator.New()

type modrinthClient interface {
	Project(ctx context.Context, slug string) (*mr.Project, error)
	Versions(ctx context.Context, slug, gameVersion, loader string) ([]mr.Version, error)
	Search(ctx context.Context, query string) (*mr.SearchResult, error)
}

var modClient modrinthClient = mr.NewClient()

var lastSync atomic.Int64
var latencyP50 atomic.Int64
var latencyP95 atomic.Int64

var latencyMu sync.Mutex
var latencySamples []int64

var writeLimiter = rate.NewLimiter(rate.Every(time.Second), 5)

var csrfToken string

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

type metadataRequest struct {
	URL string `json:"url" validate:"required,url"`
}

func validatePayload(v interface{}) *httpx.HTTPError {
	if err := validate.Struct(v); err != nil {
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			fields := make(map[string]string, len(ve))
			for _, fe := range ve {
				fields[strings.ToLower(fe.Field())] = fe.Tag()
			}
			return httpx.BadRequest("validation failed").WithFields(fields)
		}
		return httpx.Internal(err)
	}
	return nil
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

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'none'")
		next.ServeHTTP(w, r)
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

// New builds a router with all HTTP handlers.
func New(db *sql.DB, dist embed.FS) http.Handler {
	km, err := secrets.Load(context.Background())
	if err != nil {
		log.Fatal().Err(err).Msg("load secrets")
	}
	svc := secrets.NewService(db, km)
	tokenpkg.Init(svc)
	pppkg.Init(svc)

	r := chi.NewRouter()

	r.Use(securityHeaders)
	r.Use(recordLatency)

	r.Get("/favicon.ico", serveFavicon(dist))
	r.Get("/api/instances", listInstancesHandler(db))
	r.Get("/api/instances/{id}", getInstanceHandler(db))
	r.Post("/api/instances", createInstanceHandler(db))
	r.Put("/api/instances/{id}", updateInstanceHandler(db))
	r.Post("/api/instances/{id}/resync", resyncInstanceHandler(db))
	r.Delete("/api/instances/{id}", deleteInstanceHandler(db))
	r.Get("/api/mods", listModsHandler(db))
	r.Post("/api/mods/metadata", metadataHandler())
	r.Post("/api/mods", createModHandler(db))
	r.Put("/api/mods/{id}", updateModHandler(db))
	r.Delete("/api/mods/{id}", deleteModHandler(db))
	r.Post("/api/mods/{id}/update", applyUpdateHandler(db))
	r.Get("/api/pufferpanel", getPufferHandler())
	r.Post("/api/pufferpanel/test", testPufferHandler())
	r.Get("/api/pufferpanel/servers", listServersHandler())
	r.Post("/api/pufferpanel/sync", syncHandler(db))

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

func serveFavicon(dist embed.FS) http.HandlerFunc {
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

func createInstanceHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name                string `json:"name"`
			Loader              string `json:"loader"`
			EnforceSameLoader   *bool  `json:"enforce_same_loader"`
			PufferpanelServerID string `json:"pufferpanel_server_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid json"))
			return
		}
		inst := dbpkg.Instance{ID: 0, Name: req.Name, Loader: req.Loader, PufferpanelServerID: req.PufferpanelServerID}
		if req.EnforceSameLoader == nil {
			inst.EnforceSameLoader = true
		} else {
			inst.EnforceSameLoader = *req.EnforceSameLoader
		}
		if err := validatePayload(&inst); err != nil {
			httpx.Write(w, r, err)
			return
		}
		if err := dbpkg.InsertInstance(db, &inst); err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
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
			inst.Name = *req.Name
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
	DeepScan     bool   `json:"deep_scan"`
}

func getPufferHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		creds, err := pppkg.Config()
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(creds)
	}
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
			if err := pppkg.Set(pppkg.Credentials{BaseURL: req.BaseURL, ClientID: req.ClientID, ClientSecret: req.ClientSecret, DeepScan: req.DeepScan}); err != nil {
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
		exists, last4, updatedAt, err := svc.Status(r.Context(), typ)
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
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
		var req pufferRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid json"))
			return
		}
		if err := validatePayload(&req); err != nil {
			httpx.Write(w, r, err)
			return
		}
		if err := pppkg.TestConnection(r.Context(), pppkg.Credentials{BaseURL: req.BaseURL, ClientID: req.ClientID, ClientSecret: req.ClientSecret, DeepScan: req.DeepScan}); err != nil {
			httpx.Write(w, r, httpx.BadRequest(err.Error()))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func listServersHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		servers, err := pppkg.ListServers(r.Context())
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(servers)
	}
}

func syncHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serverID := r.URL.Query().Get("server")
		if serverID == "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		srv, err := pppkg.GetServer(r.Context(), serverID)
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		inst := dbpkg.Instance{
			Name:                srv.Name,
			Loader:              strings.ToLower(srv.Environment.Type),
			PufferpanelServerID: serverID,
			EnforceSameLoader:   true,
		}
		if inst.Loader == "" {
			inst.Loader = "fabric"
		}
		if err := validatePayload(&inst); err != nil {
			httpx.Write(w, r, err)
			return
		}
		if err := dbpkg.InsertInstance(db, &inst); err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		creds, err := pppkg.Get()
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		files, err := pppkg.ListJarFiles(r.Context(), serverID)
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		matched := make([]dbpkg.Mod, 0)
		unmatched := make([]string, 0, len(files))
		for _, f := range files {
			slug, ver := parseJarFilename(f)
			scanned := false
			if creds.DeepScan && (slug == "" || ver == "") {
				scanned = true
				time.Sleep(100 * time.Millisecond)
				data, err := pppkg.FetchFile(r.Context(), serverID, "mods/"+f)
				if errors.Is(err, os.ErrNotExist) {
					data, err = pppkg.FetchFile(r.Context(), serverID, "plugins/"+f)
				}
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
				data, err := pppkg.FetchFile(r.Context(), serverID, "mods/"+f)
				if errors.Is(err, os.ErrNotExist) {
					data, err = pppkg.FetchFile(r.Context(), serverID, "plugins/"+f)
				}
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

func resyncInstanceHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid id"))
			return
		}
		inst, err := dbpkg.GetInstance(db, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if inst.PufferpanelServerID == "" {
			httpx.Write(w, r, httpx.BadRequest("no pufferpanel server"))
			return
		}
		creds, err := pppkg.Get()
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		files, err := pppkg.ListJarFiles(r.Context(), inst.PufferpanelServerID)
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		existing, err := dbpkg.ListMods(db, inst.ID)
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		exMap := map[string]dbpkg.Mod{}
		for _, m := range existing {
			slug, _ := parseModrinthSlug(m.URL)
			exMap[slug] = m
		}
		added, changed := 0, 0
		unmatched := []string{}
		for _, f := range files {
			slug, ver := parseJarFilename(f)
			scanned := false
			if creds.DeepScan && (slug == "" || ver == "") {
				scanned = true
				time.Sleep(100 * time.Millisecond)
				data, err := pppkg.FetchFile(r.Context(), inst.PufferpanelServerID, "mods/"+f)
				if errors.Is(err, os.ErrNotExist) {
					data, err = pppkg.FetchFile(r.Context(), inst.PufferpanelServerID, "plugins/"+f)
				}
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
				data, err := pppkg.FetchFile(r.Context(), inst.PufferpanelServerID, "mods/"+f)
				if errors.Is(err, os.ErrNotExist) {
					data, err = pppkg.FetchFile(r.Context(), inst.PufferpanelServerID, "plugins/"+f)
				}
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
			if existingMod, ok := exMap[slug]; ok {
				if existingMod.CurrentVersion == ver {
					continue
				}
				existingMod.CurrentVersion = v.VersionNumber
				existingMod.Channel = strings.ToLower(v.VersionType)
				existingMod.DownloadURL = ""
				if len(v.Files) > 0 {
					existingMod.DownloadURL = v.Files[0].URL
				}
				if len(v.GameVersions) > 0 {
					existingMod.GameVersion = v.GameVersions[0]
				}
				if len(v.Loaders) > 0 {
					existingMod.Loader = v.Loaders[0]
				}
				if err := populateAvailableVersion(r.Context(), &existingMod, slug); err != nil {
					unmatched = append(unmatched, f)
					continue
				}
				if err := dbpkg.UpdateMod(db, &existingMod); err != nil {
					unmatched = append(unmatched, f)
					continue
				}
				if err := dbpkg.InsertUpdateIfNew(db, existingMod.ID, existingMod.CurrentVersion); err != nil {
					unmatched = append(unmatched, f)
					continue
				}
				changed++
				exMap[slug] = existingMod
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
			added++
		}
		if err := dbpkg.UpdateInstanceSync(db, inst.ID, added, changed, len(unmatched)); err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		inst2, err := dbpkg.GetInstance(db, inst.ID)
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		mods, err := dbpkg.ListMods(db, inst.ID)
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Instance  dbpkg.Instance `json:"instance"`
			Unmatched []string       `json:"unmatched"`
			Mods      []dbpkg.Mod    `json:"mods"`
		}{*inst2, unmatched, mods})
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
			http.NotFound(w, r)
			return
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

func parseJarFilename(name string) (slug, version string) {
	name = strings.TrimSuffix(strings.ToLower(name), ".jar")
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' })
	if len(parts) == 0 {
		return "", ""
	}
	idx := -1
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" && parts[i][0] >= '0' && parts[i][0] <= '9' {
			idx = i
			break
		}
	}
	slugParts := parts
	if idx != -1 {
		version = parts[idx]
		slugParts = parts[:idx]
	}
	filtered := make([]string, 0, len(slugParts))
	loaders := map[string]struct{}{"fabric": {}, "forge": {}, "quilt": {}, "neoforge": {}}
	for _, p := range slugParts {
		if _, ok := loaders[p]; ok {
			continue
		}
		if strings.HasPrefix(p, "mc") {
			continue
		}
		filtered = append(filtered, p)
	}
	if len(filtered) == 0 {
		filtered = slugParts
	}
	slug = strings.Join(filtered, "-")
	return
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
