package main

import (
	"bytes"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	urlpkg "net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-co-op/gocron"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	_ "modernc.org/sqlite"
)

//go:embed frontend/dist/* favicon.ico
var distFS embed.FS

type Mod struct {
	ID               int    `json:"id"`
	Name             string `json:"name"`
	IconURL          string `json:"icon_url"`
	URL              string `json:"url"`
	GameVersion      string `json:"game_version"`
	Loader           string `json:"loader"`
	Channel          string `json:"channel"`
	CurrentVersion   string `json:"current_version"`
	AvailableVersion string `json:"available_version"`
	AvailableChannel string `json:"available_channel"`
	DownloadURL      string `json:"download_url"`
}

func resolveDBPath(p string) string {
	info, err := os.Stat(p)
	if err == nil && info.IsDir() {
		return filepath.Join(p, "mods.db")
	}
	return p
}

func ensureFile(p string) error {
	info, err := os.Stat(p)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("%s is a directory", p)
		}
		return nil
	}
	if os.IsNotExist(err) {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0666)
		if err != nil {
			return err
		}
		return f.Close()
	}
	return err
}

func main() {
	log.Logger = log.Output(zerolog.New(os.Stdout).With().Timestamp().Logger())
	path := resolveDBPath("mods.db")
	if err := ensureFile(path); err != nil {
		log.Fatal().Err(err).Str("path", path).Msg("create db file")
	}

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_busy_timeout=5000&_pragma=foreign_keys(1)", path))
	if err != nil {
		log.Fatal().Err(err).Msg("open db")
	}
	defer db.Close()

	if err := initDB(db); err != nil {
		log.Fatal().Err(err).Msg("init db")
	}

	scheduler := gocron.NewScheduler(time.UTC)
	scheduler.Every(1).Hour().Do(func() { checkUpdates(db) })
	scheduler.StartAsync()

	r := chi.NewRouter()

	r.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(distFS, "favicon.ico")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/x-icon")
		http.ServeContent(w, r, "favicon.ico", time.Now(), bytes.NewReader(data))
	})

	r.Get("/api/mods", func(w http.ResponseWriter, r *http.Request) {
		mods, err := listMods(db)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mods)
	})

	r.Post("/api/mods/metadata", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		meta, err := fetchModMetadata(req.URL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(meta)
	})

	r.Post("/api/mods", func(w http.ResponseWriter, r *http.Request) {
		var m Mod
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		slug, err := parseModrinthSlug(m.URL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := populateProjectInfo(&m, slug); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := populateVersions(&m, slug); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := insertMod(db, &m); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		mods, err := listMods(db)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mods)
	})

	static, _ := fs.Sub(distFS, "frontend/dist")
	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
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
	})

	log.Info().Msg("starting server on :8080")
	if err := http.ListenAndServe(":8080", r); err != nil {
		log.Fatal().Err(err).Msg("server failed")
	}
}

func initDB(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS mods (
       id INTEGER PRIMARY KEY AUTOINCREMENT,
       name TEXT,
       icon_url TEXT,
       url TEXT NOT NULL,
       game_version TEXT,
       loader TEXT,
       channel TEXT,
       current_version TEXT,
       available_version TEXT,
       available_channel TEXT,
       download_url TEXT
   )`)
	if err != nil {
		return err
	}

	columns := map[string]string{
		"name":              "TEXT",
		"icon_url":          "TEXT",
		"game_version":      "TEXT",
		"loader":            "TEXT",
		"channel":           "TEXT",
		"current_version":   "TEXT",
		"available_version": "TEXT",
		"available_channel": "TEXT",
		"download_url":      "TEXT",
	}

	rows, err := db.Query(`SELECT name FROM pragma_table_info('mods')`)
	if err != nil {
		return err
	}
	defer rows.Close()

	existing := make(map[string]struct{})
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return err
		}
		existing[n] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for col, typ := range columns {
		if _, ok := existing[col]; !ok {
			stmt := fmt.Sprintf(`ALTER TABLE mods ADD COLUMN %s %s`, col, typ)
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("add column %s: %w", col, err)
			}
		}
	}

	return nil
}

func insertMod(db *sql.DB, m *Mod) error {
	res, err := db.Exec(`INSERT INTO mods(name, icon_url, url, game_version, loader, channel, current_version, available_version, available_channel, download_url) VALUES(?,?,?,?,?,?,?,?,?,?)`, m.Name, m.IconURL, m.URL, m.GameVersion, m.Loader, m.Channel, m.CurrentVersion, m.AvailableVersion, m.AvailableChannel, m.DownloadURL)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err == nil {
		m.ID = int(id)
	}
	return nil
}

func listMods(db *sql.DB) ([]Mod, error) {
	rows, err := db.Query(`SELECT id, name, icon_url, url, game_version, loader, channel, current_version, available_version, available_channel, download_url FROM mods ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var mods []Mod
	for rows.Next() {
		var m Mod
		if err := rows.Scan(&m.ID, &m.Name, &m.IconURL, &m.URL, &m.GameVersion, &m.Loader, &m.Channel, &m.CurrentVersion, &m.AvailableVersion, &m.AvailableChannel, &m.DownloadURL); err != nil {
			return nil, err
		}
		mods = append(mods, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return mods, nil
}

func checkUpdates(db *sql.DB) {
	mods, err := listMods(db)
	if err != nil {
		log.Error().Err(err).Msg("list mods")
		return
	}
	for _, m := range mods {
		slug, err := parseModrinthSlug(m.URL)
		if err != nil {
			continue
		}
		if err := populateAvailableVersion(&m, slug); err != nil {
			continue
		}
		_, err = db.Exec(`UPDATE mods SET available_version=?, available_channel=?, download_url=? WHERE id=?`, m.AvailableVersion, m.AvailableChannel, m.DownloadURL, m.ID)
		if err != nil {
			log.Error().Err(err).Msg("update version")
		}
	}
}

type ModVersion struct {
	GameVersions []string `json:"game_versions"`
	Loaders      []string `json:"loaders"`
	VersionType  string   `json:"version_type"`
}

type ModMetadata struct {
	GameVersions []string     `json:"game_versions"`
	Loaders      []string     `json:"loaders"`
	Channels     []string     `json:"channels"`
	Versions     []ModVersion `json:"versions"`
}

func fetchModMetadata(rawURL string) (*ModMetadata, error) {
	slug, err := parseModrinthSlug(rawURL)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("https://api.modrinth.com/v2/project/%s/version", slug)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("modrinth status: %s", resp.Status)
	}
	var versions []ModVersion
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return nil, err
	}
	gvSet := map[string]struct{}{}
	ldSet := map[string]struct{}{}
	chSet := map[string]struct{}{}
	meta := &ModMetadata{}
	for _, v := range versions {
		mv := ModVersion{
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

type modrinthVersion struct {
	VersionNumber string `json:"version_number"`
	VersionType   string `json:"version_type"`
	Files         []struct {
		URL string `json:"url"`
	} `json:"files"`
}

func populateProjectInfo(m *Mod, slug string) error {
	url := fmt.Sprintf("https://api.modrinth.com/v2/project/%s", slug)
	resp, err := http.Get(url)
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

func fetchVersions(slug, gameVersion, loader string) ([]modrinthVersion, error) {
	url := fmt.Sprintf("https://api.modrinth.com/v2/project/%s/version?game_versions=[\"%s\"]&loaders=[\"%s\"]", slug, gameVersion, loader)
	resp, err := http.Get(url)
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

func populateVersions(m *Mod, slug string) error {
	versions, err := fetchVersions(slug, m.GameVersion, m.Loader)
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
	// determine available version/channel
	if err := populateAvailableVersion(m, slug); err != nil {
		return err
	}
	return nil
}

func populateAvailableVersion(m *Mod, slug string) error {
	versions, err := fetchVersions(slug, m.GameVersion, m.Loader)
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
	// fallback to current
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
