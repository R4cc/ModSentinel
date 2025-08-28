package db

import (
	"database/sql"
	"fmt"
)

const InstanceNameMaxLen = 128

// Instance represents a game instance tracking mods.
type Instance struct {
	ID                  int    `json:"id"`
	Name                string `json:"name" validate:"max=128"`
	Loader              string `json:"loader"`
	PufferpanelServerID string `json:"pufferpanel_server_id"`
	EnforceSameLoader   bool   `json:"enforce_same_loader"`
	CreatedAt           string `json:"created_at"`
	ModCount            int    `json:"mod_count"`
	LastSyncAt          string `json:"last_sync_at"`
	LastSyncAdded       int    `json:"last_sync_added"`
	LastSyncUpdated     int    `json:"last_sync_updated"`
	LastSyncFailed      int    `json:"last_sync_failed"`
}

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
	InstanceID       int    `json:"instance_id"`
}

// ModUpdate represents a recently applied mod update.
type ModUpdate struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	UpdatedAt string `json:"updated_at"`
}

// Init ensures the mods and instances tables exist and have required columns.
func Init(db *sql.DB) error {
	_, err := db.Exec(fmt.Sprintf(`CREATE TABLE IF NOT EXISTS instances (
       id INTEGER PRIMARY KEY AUTOINCREMENT,
       name TEXT NOT NULL CHECK(length(name) <= %d AND length(trim(name)) > 0),
       loader TEXT,
       enforce_same_loader INTEGER DEFAULT 1,
       created_at DATETIME DEFAULT CURRENT_TIMESTAMP
   )`, InstanceNameMaxLen))
	if err != nil {
		return err
	}

	instCols := map[string]string{
		"name":                  fmt.Sprintf("TEXT NOT NULL CHECK(length(name) <= %d AND length(trim(name)) > 0)", InstanceNameMaxLen),
		"loader":                "TEXT",
		"pufferpanel_server_id": "TEXT",
		"enforce_same_loader":   "INTEGER DEFAULT 1",
		"created_at":            "DATETIME DEFAULT CURRENT_TIMESTAMP",
		"last_sync_at":          "DATETIME",
		"last_sync_added":       "INTEGER DEFAULT 0",
		"last_sync_updated":     "INTEGER DEFAULT 0",
		"last_sync_failed":      "INTEGER DEFAULT 0",
	}

	rows, err := db.Query(`SELECT name FROM pragma_table_info('instances')`)
	if err != nil {
		return err
	}
	existingInst := make(map[string]struct{})
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return err
		}
		existingInst[n] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	for col, typ := range instCols {
		if _, ok := existingInst[col]; !ok {
			stmt := fmt.Sprintf(`ALTER TABLE instances ADD COLUMN %s %s`, col, typ)
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("add column %s: %w", col, err)
			}
		}
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS mods (
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
       download_url TEXT,
       instance_id INTEGER
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
		"instance_id":       "INTEGER",
	}

	rows, err = db.Query(`SELECT name FROM pragma_table_info('mods')`)
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

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS updates (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        mod_id INTEGER NOT NULL,
        version TEXT,
        updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
    )`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS secrets (
       name TEXT PRIMARY KEY,
       value BLOB NOT NULL,
       created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
       updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
   )`)
	if err != nil {
		return err
	}

	secretCols := map[string]string{
		"value":      "BLOB NOT NULL",
		"created_at": "DATETIME DEFAULT CURRENT_TIMESTAMP",
		"updated_at": "DATETIME DEFAULT CURRENT_TIMESTAMP",
	}

	rows2, err := db.Query(`SELECT name FROM pragma_table_info('secrets')`)
	if err != nil {
		return err
	}
	existingSecret := make(map[string]struct{})
	for rows2.Next() {
		var n string
		if err := rows2.Scan(&n); err != nil {
			rows2.Close()
			return err
		}
		existingSecret[n] = struct{}{}
	}
	if err := rows2.Err(); err != nil {
		rows2.Close()
		return err
	}
	rows2.Close()
	for col, typ := range secretCols {
		if _, ok := existingSecret[col]; !ok {
			stmt := fmt.Sprintf(`ALTER TABLE secrets ADD COLUMN %s %s`, col, typ)
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("add column %s: %w", col, err)
			}
		}
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS app_settings (
       key TEXT PRIMARY KEY,
       value TEXT,
       updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
   )`)
	if err != nil {
		return err
	}

	// Migration: create a default instance and assign existing mods.
	var instCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM instances`).Scan(&instCount); err != nil {
		return err
	}
	if instCount == 0 {
		rows, err := db.Query(`SELECT DISTINCT loader FROM mods WHERE IFNULL(loader, '') <> ''`)
		if err != nil {
			return err
		}
		loaders := []string{}
		for rows.Next() {
			var l string
			if err := rows.Scan(&l); err != nil {
				rows.Close()
				return err
			}
			loaders = append(loaders, l)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		instLoader := ""
		if len(loaders) == 1 {
			instLoader = loaders[0]
		}
		res, err := db.Exec(`INSERT INTO instances(name, loader, enforce_same_loader) VALUES('Default', ?, 1)`, instLoader)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := db.Exec(`UPDATE mods SET instance_id=? WHERE IFNULL(instance_id, 0)=0`, id); err != nil {
			return err
		}
	}

	return nil
}

// InsertMod inserts a new mod record.
func InsertMod(db *sql.DB, m *Mod) error {
	res, err := db.Exec(`INSERT INTO mods(name, icon_url, url, game_version, loader, channel, current_version, available_version, available_channel, download_url, instance_id) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, m.Name, m.IconURL, m.URL, m.GameVersion, m.Loader, m.Channel, m.CurrentVersion, m.AvailableVersion, m.AvailableChannel, m.DownloadURL, m.InstanceID)
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
	_, err := db.Exec(`UPDATE mods SET name=?, icon_url=?, url=?, game_version=?, loader=?, channel=?, current_version=?, available_version=?, available_channel=?, download_url=?, instance_id=? WHERE id=?`, m.Name, m.IconURL, m.URL, m.GameVersion, m.Loader, m.Channel, m.CurrentVersion, m.AvailableVersion, m.AvailableChannel, m.DownloadURL, m.InstanceID, m.ID)
	return err
}

// DeleteMod removes a mod by ID.
func DeleteMod(db *sql.DB, id int) error {
	_, err := db.Exec(`DELETE FROM mods WHERE id=?`, id)
	return err
}

// InsertInstance inserts a new instance record.
func InsertInstance(db *sql.DB, i *Instance) error {
	res, err := db.Exec(`INSERT INTO instances(name, loader, enforce_same_loader, pufferpanel_server_id) VALUES(?,?,?,?)`, i.Name, i.Loader, i.EnforceSameLoader, i.PufferpanelServerID)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err == nil {
		i.ID = int(id)
	}
	return nil
}

// UpdateInstance updates an existing instance.
func UpdateInstance(db *sql.DB, i *Instance) error {
	_, err := db.Exec(`UPDATE instances SET name=?, enforce_same_loader=? WHERE id=?`, i.Name, i.EnforceSameLoader, i.ID)
	return err
}

// UpdateInstanceSync records sync stats for an instance.
func UpdateInstanceSync(db *sql.DB, id, added, updated, failed int) error {
	_, err := db.Exec(`UPDATE instances SET last_sync_at=CURRENT_TIMESTAMP, last_sync_added=?, last_sync_updated=?, last_sync_failed=? WHERE id=?`, added, updated, failed, id)
	return err
}

// DeleteInstance removes an instance. If targetID is provided, mods are moved
// to the target instance before deletion; otherwise contained mods are removed.
func DeleteInstance(db *sql.DB, id int, targetID *int) error {
	if targetID != nil {
		if _, err := db.Exec(`UPDATE mods SET instance_id=? WHERE instance_id=?`, *targetID, id); err != nil {
			return err
		}
	} else {
		if _, err := db.Exec(`DELETE FROM mods WHERE instance_id=?`, id); err != nil {
			return err
		}
	}
	_, err := db.Exec(`DELETE FROM instances WHERE id=?`, id)
	return err
}

// GetInstance returns an instance by ID.
func GetInstance(db *sql.DB, id int) (*Instance, error) {
	var inst Instance
	err := db.QueryRow(`SELECT i.id, IFNULL(i.name, ''), IFNULL(i.loader, ''), IFNULL(i.pufferpanel_server_id, ''), IFNULL(i.enforce_same_loader, 1), IFNULL(i.created_at, ''), IFNULL(i.last_sync_at, ''), IFNULL(i.last_sync_added, 0), IFNULL(i.last_sync_updated, 0), IFNULL(i.last_sync_failed, 0),
             (SELECT COUNT(*) FROM mods m WHERE m.instance_id = i.id)
             FROM instances i WHERE i.id=?`, id).Scan(&inst.ID, &inst.Name, &inst.Loader, &inst.PufferpanelServerID, &inst.EnforceSameLoader, &inst.CreatedAt, &inst.LastSyncAt, &inst.LastSyncAdded, &inst.LastSyncUpdated, &inst.LastSyncFailed, &inst.ModCount)
	if err != nil {
		return nil, err
	}
	return &inst, nil
}

// ListInstances returns all instances sorted by ID descending.
func ListInstances(db *sql.DB) ([]Instance, error) {
	rows, err := db.Query(`SELECT i.id, IFNULL(i.name, ''), IFNULL(i.loader, ''), IFNULL(i.pufferpanel_server_id, ''), IFNULL(i.enforce_same_loader, 1), IFNULL(i.created_at, ''), IFNULL(i.last_sync_at, ''), IFNULL(i.last_sync_added, 0), IFNULL(i.last_sync_updated, 0), IFNULL(i.last_sync_failed, 0), COUNT(m.id)
              FROM instances i LEFT JOIN mods m ON m.instance_id = i.id GROUP BY i.id ORDER BY i.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Instance{}
	for rows.Next() {
		var inst Instance
		if err := rows.Scan(&inst.ID, &inst.Name, &inst.Loader, &inst.PufferpanelServerID, &inst.EnforceSameLoader, &inst.CreatedAt, &inst.LastSyncAt, &inst.LastSyncAdded, &inst.LastSyncUpdated, &inst.LastSyncFailed, &inst.ModCount); err != nil {
			return nil, err
		}
		out = append(out, inst)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ListMods returns mods for the provided instance sorted by ID descending.
func ListMods(db *sql.DB, instanceID int) ([]Mod, error) {
	rows, err := db.Query(`SELECT id, IFNULL(name, ''), IFNULL(icon_url, ''), url, IFNULL(game_version, ''), IFNULL(loader, ''), IFNULL(channel, ''), IFNULL(current_version, ''), IFNULL(available_version, ''), IFNULL(available_channel, ''), IFNULL(download_url, ''), IFNULL(instance_id, 0) FROM mods WHERE instance_id=? ORDER BY id DESC`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	mods := []Mod{}
	for rows.Next() {
		var m Mod
		if err := rows.Scan(&m.ID, &m.Name, &m.IconURL, &m.URL, &m.GameVersion, &m.Loader, &m.Channel, &m.CurrentVersion, &m.AvailableVersion, &m.AvailableChannel, &m.DownloadURL, &m.InstanceID); err != nil {
			return nil, err
		}
		mods = append(mods, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return mods, nil
}

// ListAllMods returns all mods across instances sorted by ID descending.
func ListAllMods(db *sql.DB) ([]Mod, error) {
	rows, err := db.Query(`SELECT id, IFNULL(name, ''), IFNULL(icon_url, ''), url, IFNULL(game_version, ''), IFNULL(loader, ''), IFNULL(channel, ''), IFNULL(current_version, ''), IFNULL(available_version, ''), IFNULL(available_channel, ''), IFNULL(download_url, ''), IFNULL(instance_id, 0) FROM mods ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	mods := []Mod{}
	for rows.Next() {
		var m Mod
		if err := rows.Scan(&m.ID, &m.Name, &m.IconURL, &m.URL, &m.GameVersion, &m.Loader, &m.Channel, &m.CurrentVersion, &m.AvailableVersion, &m.AvailableChannel, &m.DownloadURL, &m.InstanceID); err != nil {
			return nil, err
		}
		mods = append(mods, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return mods, nil
}

// GetMod returns a mod by ID.
func GetMod(db *sql.DB, id int) (*Mod, error) {
	var m Mod
	err := db.QueryRow(`SELECT id, IFNULL(name, ''), IFNULL(icon_url, ''), url, IFNULL(game_version, ''), IFNULL(loader, ''), IFNULL(channel, ''), IFNULL(current_version, ''), IFNULL(available_version, ''), IFNULL(available_channel, ''), IFNULL(download_url, ''), IFNULL(instance_id, 0) FROM mods WHERE id=?`, id).Scan(&m.ID, &m.Name, &m.IconURL, &m.URL, &m.GameVersion, &m.Loader, &m.Channel, &m.CurrentVersion, &m.AvailableVersion, &m.AvailableChannel, &m.DownloadURL, &m.InstanceID)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// ApplyUpdate sets current version to the available version.
func ApplyUpdate(db *sql.DB, id int) (*Mod, error) {
	m, err := GetMod(db, id)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`UPDATE mods SET current_version=available_version, channel=available_channel WHERE id=?`, id); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`INSERT INTO updates(mod_id, version) VALUES(?, ?)`, id, m.AvailableVersion); err != nil {
		return nil, err
	}
	return GetMod(db, id)
}

// InsertUpdateIfNew records a mod update if the version hasn't been recorded.
func InsertUpdateIfNew(db *sql.DB, modID int, version string) error {
	_, err := db.Exec(`INSERT INTO updates(mod_id, version)
               SELECT ?, ? WHERE NOT EXISTS (SELECT 1 FROM updates WHERE mod_id=? AND version=?)`, modID, version, modID, version)
	return err
}

// DashboardStats aggregates counts and recent updates for the dashboard.
type DashboardStats struct {
	Tracked       int
	UpToDate      int
	Outdated      int
	OutdatedMods  []Mod
	RecentUpdates []ModUpdate
}

// GetDashboardStats returns dashboard metrics.
func GetDashboardStats(db *sql.DB) (*DashboardStats, error) {
	stats := &DashboardStats{}

	if err := db.QueryRow(`SELECT COUNT(*) FROM mods`).Scan(&stats.Tracked); err != nil {
		return nil, err
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM mods WHERE IFNULL(current_version, '') = IFNULL(available_version, '')`).Scan(&stats.UpToDate); err != nil {
		return nil, err
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM mods WHERE IFNULL(current_version, '') <> IFNULL(available_version, '')`).Scan(&stats.Outdated); err != nil {
		return nil, err
	}

	rows, err := db.Query(`SELECT id, IFNULL(name, ''), IFNULL(icon_url, ''), url, IFNULL(game_version, ''), IFNULL(loader, ''), IFNULL(channel, ''), IFNULL(current_version, ''), IFNULL(available_version, ''), IFNULL(available_channel, ''), IFNULL(download_url, ''), IFNULL(instance_id, 0) FROM mods WHERE IFNULL(current_version, '') <> IFNULL(available_version, '') ORDER BY id DESC LIMIT 5`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m Mod
		if err := rows.Scan(&m.ID, &m.Name, &m.IconURL, &m.URL, &m.GameVersion, &m.Loader, &m.Channel, &m.CurrentVersion, &m.AvailableVersion, &m.AvailableChannel, &m.DownloadURL, &m.InstanceID); err != nil {
			rows.Close()
			return nil, err
		}
		stats.OutdatedMods = append(stats.OutdatedMods, m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	rows, err = db.Query(`SELECT u.mod_id, IFNULL(m.name, ''), IFNULL(u.version, ''), u.updated_at FROM updates u JOIN mods m ON u.mod_id = m.id WHERE u.updated_at >= datetime('now', '-7 day') ORDER BY u.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var u ModUpdate
		if err := rows.Scan(&u.ID, &u.Name, &u.Version, &u.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		stats.RecentUpdates = append(stats.RecentUpdates, u)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	return stats, nil
}
