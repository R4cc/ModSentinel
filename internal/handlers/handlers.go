package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	urlpkg "net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"sync"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/rs/zerolog/log"

	dbpkg "modsentinel/internal/db"
	"modsentinel/internal/httpx"
	mr "modsentinel/internal/modrinth"
	"modsentinel/internal/telemetry"
	tokenpkg "modsentinel/internal/token"
)

var validate = validator.New()

type modrinthClient interface {
	Project(ctx context.Context, slug string) (*mr.Project, error)
	Versions(ctx context.Context, slug, gameVersion, loader string) ([]mr.Version, error)
}

var modClient modrinthClient = mr.NewClient()

var lastSync atomic.Int64
var latencyP50 atomic.Int64
var latencyP95 atomic.Int64

var latencyMu sync.Mutex
var latencySamples []int64

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

// New builds a router with all HTTP handlers.
func New(db *sql.DB, dist embed.FS) http.Handler {
	r := chi.NewRouter()

	r.Use(recordLatency)

	r.Get("/favicon.ico", serveFavicon(dist))
	r.Get("/api/instances", listInstancesHandler(db))
	r.Get("/api/instances/{id}", getInstanceHandler(db))
	r.Post("/api/instances", createInstanceHandler(db))
	r.Put("/api/instances/{id}", updateInstanceHandler(db))
	r.Delete("/api/instances/{id}", deleteInstanceHandler(db))
	r.Get("/api/mods", listModsHandler(db))
	r.Post("/api/mods/metadata", metadataHandler())
	r.Post("/api/mods", createModHandler(db))
	r.Put("/api/mods/{id}", updateModHandler(db))
	r.Delete("/api/mods/{id}", deleteModHandler(db))
	r.Post("/api/mods/{id}/update", applyUpdateHandler(db))
	r.Get("/api/token", getTokenHandler())
	r.Post("/api/token", setTokenHandler())
	r.Delete("/api/token", clearTokenHandler())
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
			Name              string `json:"name"`
			Loader            string `json:"loader"`
			EnforceSameLoader *bool  `json:"enforce_same_loader"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid json"))
			return
		}
		inst := dbpkg.Instance{ID: 0, Name: req.Name, Loader: req.Loader}
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
			Name              string `json:"name"`
			Loader            string `json:"loader"`
			EnforceSameLoader bool   `json:"enforce_same_loader"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid json"))
			return
		}
		inst.Name = req.Name
		inst.Loader = req.Loader
		inst.EnforceSameLoader = req.EnforceSameLoader
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

func getTokenHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, err := tokenpkg.GetToken()
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"token": tok})
	}
}

func setTokenHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req tokenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid json"))
			return
		}
		if err := validatePayload(&req); err != nil {
			httpx.Write(w, r, err)
			return
		}
		if err := tokenpkg.SetToken(req.Token); err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		telemetry.Event("token_set", nil)
		w.WriteHeader(http.StatusNoContent)
	}
}

func clearTokenHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := tokenpkg.ClearToken(); err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		telemetry.Event("token_cleared", nil)
		w.WriteHeader(http.StatusNoContent)
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
