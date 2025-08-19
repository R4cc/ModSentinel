package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	urlpkg "net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/rs/zerolog/log"

	dbpkg "modsentinel/internal/db"
	"modsentinel/internal/httpx"
)

var validate = validator.New()

var httpClient = &http.Client{Timeout: 10 * time.Second}

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

// New builds a router with all HTTP handlers.
func New(db *sql.DB, dist embed.FS) http.Handler {
	r := chi.NewRouter()

	r.Get("/favicon.ico", serveFavicon(dist))
	r.Get("/api/mods", listModsHandler(db))
	r.Post("/api/mods/metadata", metadataHandler())
	r.Post("/api/mods", createModHandler(db))
	r.Put("/api/mods/{id}", updateModHandler(db))
	r.Delete("/api/mods/{id}", deleteModHandler(db))

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

func listModsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mods, err := dbpkg.ListMods(db)
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
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
			httpx.Write(w, r, httpx.BadRequest(err.Error()))
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
		slug, err := parseModrinthSlug(m.URL)
		if err != nil {
			httpx.Write(w, r, httpx.BadRequest(err.Error()))
			return
		}
		if err := populateProjectInfo(r.Context(), &m, slug); err != nil {
			httpx.Write(w, r, httpx.BadRequest(err.Error()))
			return
		}
		if err := populateVersions(r.Context(), &m, slug); err != nil {
			httpx.Write(w, r, httpx.BadRequest(err.Error()))
			return
		}
		if err := dbpkg.InsertMod(db, &m); err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		mods, err := dbpkg.ListMods(db)
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mods)
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
			httpx.Write(w, r, httpx.BadRequest(err.Error()))
			return
		}
		if err := populateVersions(r.Context(), &m, slug); err != nil {
			httpx.Write(w, r, httpx.BadRequest(err.Error()))
			return
		}
		if err := dbpkg.UpdateMod(db, &m); err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		mods, err := dbpkg.ListMods(db)
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
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
		if err := dbpkg.DeleteMod(db, id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		mods, err := dbpkg.ListMods(db)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mods)
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
	mods, err := dbpkg.ListMods(db)
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
}

type modrinthVersion struct {
	VersionNumber string `json:"version_number"`
	VersionType   string `json:"version_type"`
	Files         []struct {
		URL string `json:"url"`
	} `json:"files"`
}

type modVersion struct {
	GameVersions []string `json:"game_versions"`
	Loaders      []string `json:"loaders"`
	VersionType  string   `json:"version_type"`
}

type modMetadata struct {
	GameVersions []string     `json:"game_versions"`
	Loaders      []string     `json:"loaders"`
	Channels     []string     `json:"channels"`
	Versions     []modVersion `json:"versions"`
}

func fetchModMetadata(ctx context.Context, rawURL string) (*modMetadata, error) {
	slug, err := parseModrinthSlug(rawURL)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("https://api.modrinth.com/v2/project/%s/version", slug)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("modrinth status: %s", resp.Status)
	}
	var versions []modVersion
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return nil, err
	}
	meta := &modMetadata{}
	gvSet := map[string]struct{}{}
	ldSet := map[string]struct{}{}
	chSet := map[string]struct{}{}
	for _, v := range versions {
		mv := modVersion{
			GameVersions: v.GameVersions,
			Loaders:      v.Loaders,
			VersionType:  strings.ToLower(v.VersionType),
		}
		meta.Versions = append(meta.Versions, mv)
		for _, gv := range v.GameVersions {
			gvSet[gv] = struct{}{}
		}
		for _, ld := range v.Loaders {
			ldSet[ld] = struct{}{}
		}
		chSet[mv.VersionType] = struct{}{}
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

type projectInfo struct {
	Title   string `json:"title"`
	IconURL string `json:"icon_url"`
}

func populateProjectInfo(ctx context.Context, m *dbpkg.Mod, slug string) error {
	url := fmt.Sprintf("https://api.modrinth.com/v2/project/%s", slug)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("modrinth status: %s", resp.Status)
	}
	var info projectInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return err
	}
	m.Name = info.Title
	m.IconURL = info.IconURL
	return nil
}

func fetchVersions(ctx context.Context, slug, gameVersion, loader string) ([]modrinthVersion, error) {
	url := fmt.Sprintf("https://api.modrinth.com/v2/project/%s/version?game_versions=[\"%s\"]&loaders=[\"%s\"]", slug, gameVersion, loader)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("modrinth status: %s", resp.Status)
	}
	var versions []modrinthVersion
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return nil, err
	}
	return versions, nil
}

func populateVersions(ctx context.Context, m *dbpkg.Mod, slug string) error {
	versions, err := fetchVersions(ctx, slug, m.GameVersion, m.Loader)
	if err != nil {
		return err
	}
	var current modrinthVersion
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
	versions, err := fetchVersions(ctx, slug, m.GameVersion, m.Loader)
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
