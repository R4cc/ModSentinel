package db

import (
	"database/sql"
	"fmt"
)

// Mod represents a tracked mod entry.
type Mod struct {
	ID               int    `json:"id"`
	Name             string `json:"name"`
	IconURL          string `json:"icon_url"`
	URL              string `json:"url" validate:"required,url"`
	GameVersion      string `json:"game_version"`
	Loader           string `json:"loader"`
	Channel          string `json:"channel"`
	CurrentVersion   string `json:"current_version"`
	AvailableVersion string `json:"available_version"`
	AvailableChannel string `json:"available_channel"`
	DownloadURL      string `json:"download_url"`
}

// Init ensures the mods table exists and has required columns.
func Init(db *sql.DB) error {
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

// InsertMod inserts a new mod record.
func InsertMod(db *sql.DB, m *Mod) error {
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

// UpdateMod updates an existing mod.
func UpdateMod(db *sql.DB, m *Mod) error {
	_, err := db.Exec(`UPDATE mods SET name=?, icon_url=?, url=?, game_version=?, loader=?, channel=?, current_version=?, available_version=?, available_channel=?, download_url=? WHERE id=?`, m.Name, m.IconURL, m.URL, m.GameVersion, m.Loader, m.Channel, m.CurrentVersion, m.AvailableVersion, m.AvailableChannel, m.DownloadURL, m.ID)
	return err
}

// DeleteMod removes a mod by ID.
func DeleteMod(db *sql.DB, id int) error {
	_, err := db.Exec(`DELETE FROM mods WHERE id=?`, id)
	return err
}

// ListMods returns all mods sorted by ID descending.
func ListMods(db *sql.DB) ([]Mod, error) {
	rows, err := db.Query(`SELECT id, IFNULL(name, ''), IFNULL(icon_url, ''), url, IFNULL(game_version, ''), IFNULL(loader, ''), IFNULL(channel, ''), IFNULL(current_version, ''), IFNULL(available_version, ''), IFNULL(available_channel, ''), IFNULL(download_url, '') FROM mods ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	mods := []Mod{}
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
