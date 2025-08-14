package main

import (
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	urlpkg "net/url"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-co-op/gocron"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	_ "modernc.org/sqlite"
)

//go:embed templates/*
var templateFS embed.FS

var templates = template.Must(template.ParseFS(templateFS, "templates/index.html", "templates/mod_list.html"))

type Mod struct {
	ID            int
	URL           string
	GameVersion   string
	Loader        string
	Channel       string
	LatestVersion string
}

func main() {
	log.Logger = log.Output(zerolog.New(os.Stdout).With().Timestamp().Logger())

	db, err := sql.Open("sqlite", "file:mods.db?_busy_timeout=5000&_pragma=foreign_keys(1)")
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
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		mods, err := listMods(db)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := templates.ExecuteTemplate(w, "index.html", mods); err != nil {
			log.Error().Err(err).Msg("render index")
		}
	})
	r.Post("/mods", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		m := Mod{
			URL:         r.FormValue("url"),
			GameVersion: r.FormValue("game_version"),
			Loader:      r.FormValue("loader"),
			Channel:     r.FormValue("channel"),
		}
		v, err := fetchLatestVersion(m)
		if err == nil {
			m.LatestVersion = v
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
		if r.Header.Get("HX-Request") != "" {
			if err := templates.ExecuteTemplate(w, "mod_list.html", mods); err != nil {
				log.Error().Err(err).Msg("render list")
			}
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	log.Info().Msg("starting server on :8080")
	if err := http.ListenAndServe(":8080", r); err != nil {
		log.Fatal().Err(err).Msg("server failed")
	}
}

func initDB(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS mods (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        url TEXT NOT NULL,
        game_version TEXT,
        loader TEXT,
        channel TEXT,
        latest_version TEXT
    )`)
	return err
}

func insertMod(db *sql.DB, m *Mod) error {
	res, err := db.Exec(`INSERT INTO mods(url, game_version, loader, channel, latest_version) VALUES(?,?,?,?,?)`, m.URL, m.GameVersion, m.Loader, m.Channel, m.LatestVersion)
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
	rows, err := db.Query(`SELECT id, url, game_version, loader, channel, latest_version FROM mods ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var mods []Mod
	for rows.Next() {
		var m Mod
		if err := rows.Scan(&m.ID, &m.URL, &m.GameVersion, &m.Loader, &m.Channel, &m.LatestVersion); err != nil {
			return nil, err
		}
		mods = append(mods, m)
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
		v, err := fetchLatestVersion(m)
		if err != nil || v == "" || v == m.LatestVersion {
			continue
		}
		log.Info().Str("mod", m.URL).Str("new", v).Msg("update found")
		_, err = db.Exec(`UPDATE mods SET latest_version=? WHERE id=?`, v, m.ID)
		if err != nil {
			log.Error().Err(err).Msg("update version")
		}
	}
}

func fetchLatestVersion(m Mod) (string, error) {
	slug, err := parseModrinthSlug(m.URL)
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("https://api.modrinth.com/v2/project/%s/version?game_versions=[\"%s\"]&loaders=[\"%s\"]", slug, m.GameVersion, m.Loader)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("modrinth status: %s", resp.Status)
	}
	var versions []struct {
		VersionNumber string `json:"version_number"`
		VersionType   string `json:"version_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return "", err
	}
	for _, v := range versions {
		if strings.EqualFold(v.VersionType, m.Channel) {
			return v.VersionNumber, nil
		}
	}
	return "", errors.New("no version found")
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
