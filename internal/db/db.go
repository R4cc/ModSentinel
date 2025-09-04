package db

import (
        "database/sql"
        "fmt"
        "strings"
        "time"
)

const InstanceNameMaxLen = 128

// Instance represents a game instance tracking mods.
type Instance struct {
    ID                  int    `json:"id"`
    Name                string `json:"name" validate:"max=128"`
    Loader              string `json:"loader"`
    PufferpanelServerID string `json:"pufferpanel_server_id"`
    // GameVersion stores the detected game (Minecraft) version for this instance.
    GameVersion         string `json:"game_version"`
    // PufferVersionKey records the template variable key used to derive the version.
    PufferVersionKey    string `json:"puffer_version_key"`
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

// ModEvent represents an instance activity log entry for mods.
type ModEvent struct {
    ID         int    `json:"id"`
    InstanceID int    `json:"instance_id"`
    ModID      *int   `json:"mod_id,omitempty"`
    Action     string `json:"action"`
    ModName    string `json:"mod_name"`
    From       string `json:"from_version,omitempty"`
    To         string `json:"to_version,omitempty"`
    CreatedAt  string `json:"created_at"`
}

// ModSyncState tracks the last sync attempt for a mod on an instance.
type ModSyncState struct {
        Slug        string `json:"slug"`
        LastChecked string `json:"last_checked"`
        LastVersion string `json:"last_version"`
        Status      string `json:"status"`
}

// Init ensures the mods and instances tables exist and have required columns.
func Init(db *sql.DB) error {
    _, err := db.Exec(fmt.Sprintf(`CREATE TABLE IF NOT EXISTS instances (
       id INTEGER PRIMARY KEY AUTOINCREMENT,
       name TEXT NOT NULL CHECK(length(name) <= %d AND length(trim(name)) > 0),
       loader TEXT,
       created_at DATETIME DEFAULT CURRENT_TIMESTAMP
   )`, InstanceNameMaxLen))
	if err != nil {
		return err
	}

    instCols := map[string]string{
        "name":                  fmt.Sprintf("TEXT NOT NULL CHECK(length(name) <= %d AND length(trim(name)) > 0)", InstanceNameMaxLen),
        "loader":                "TEXT",
        "pufferpanel_server_id": "TEXT",
        "game_version":          "TEXT",
        "puffer_version_key":    "TEXT",
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
    // Helpful index for filtering/grouping by game version
    if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS instances_game_version_idx ON instances(game_version)`); err != nil {
        return err
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
		"installed_file":    "TEXT",
		"installed_version": "TEXT",
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

    // Track update jobs with status and timing to support idempotency and auditing
    _, err = db.Exec(`CREATE TABLE IF NOT EXISTS mod_updates (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        mod_id INTEGER NOT NULL,
        from_version TEXT,
        to_version TEXT,
        status TEXT,
        idempotency_key TEXT NOT NULL,
        started_at DATETIME,
        ended_at DATETIME,
        error TEXT,
        UNIQUE(idempotency_key)
    )`)
    if err != nil {
        return err
    }

    _, err = db.Exec(`CREATE TABLE IF NOT EXISTS secrets (
       name TEXT PRIMARY KEY,
       value BLOB NOT NULL DEFAULT X'' ,
       created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
       updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
   )`)
	if err != nil {
		return err
	}

    secretCols := map[string]string{
        "value":      "BLOB NOT NULL DEFAULT X''",
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

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS sync_jobs (
     id INTEGER PRIMARY KEY AUTOINCREMENT,
     instance_id INTEGER NOT NULL,
     server_id TEXT NOT NULL,
     status TEXT NOT NULL,
     error TEXT,
     idempotency_key TEXT NOT NULL DEFAULT '',
     created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
     started_at DATETIME,
     finished_at DATETIME,
     UNIQUE(instance_id, idempotency_key)
 )`)
	if err != nil {
		return err
	}

	rows, err = db.Query(`SELECT name FROM pragma_table_info('sync_jobs')`)
	if err != nil {
		return err
	}
	existingSJ := make(map[string]struct{})
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return err
		}
		existingSJ[n] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if _, ok := existingSJ["idempotency_key"]; !ok {
		if _, err := db.Exec(`ALTER TABLE sync_jobs ADD COLUMN idempotency_key TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
		if _, err := db.Exec(`UPDATE sync_jobs SET idempotency_key=CAST(id AS TEXT) WHERE idempotency_key=''`); err != nil {
			return err
		}
	}
        if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS sync_jobs_instance_key_idx ON sync_jobs(instance_id, idempotency_key)`); err != nil {
                return err
        }

        _, err = db.Exec(`CREATE TABLE IF NOT EXISTS mod_sync_state (
     instance_id INTEGER NOT NULL,
     slug TEXT NOT NULL,
     last_checked_at DATETIME,
     last_version TEXT,
     status TEXT,
     PRIMARY KEY(instance_id, slug)
 )`)
        if err != nil {
                return err
        }

        // Activity log table for instance mod changes
        _, err = db.Exec(`CREATE TABLE IF NOT EXISTS mod_events (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            instance_id INTEGER NOT NULL,
            mod_id INTEGER,
            action TEXT NOT NULL,
            mod_name TEXT NOT NULL,
            from_version TEXT,
            to_version TEXT,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )`)
        if err != nil {
            return err
        }

        // Slug alias map per instance: alias (normalized candidate) -> canonical slug
        _, err = db.Exec(`CREATE TABLE IF NOT EXISTS slug_aliases (
            instance_id INTEGER NOT NULL,
            alias TEXT NOT NULL,
            slug TEXT NOT NULL,
            PRIMARY KEY(instance_id, alias)
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
        res, err := db.Exec(`INSERT INTO instances(name, loader) VALUES('Default', ?)`, instLoader)
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

// SetInstalledState persists the currently installed file path and version for a mod.
func SetInstalledState(db *sql.DB, modID int, file, version string) error {
    _, err := db.Exec(`UPDATE mods SET installed_file=?, installed_version=? WHERE id=?`, file, version, modID)
    return err
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
    res, err := db.Exec(`INSERT INTO instances(name, loader, pufferpanel_server_id) VALUES(?,?,?)`, i.Name, i.Loader, i.PufferpanelServerID)
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
    // Update core editable fields including loader. Also persist optional game_version and puffer_version_key
    _, err := db.Exec(`UPDATE instances SET name=?, loader=?, game_version=?, puffer_version_key=? WHERE id=?`, i.Name, i.Loader, i.GameVersion, i.PufferVersionKey, i.ID)
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
    err := db.QueryRow(`SELECT i.id, IFNULL(i.name, ''), IFNULL(i.loader, ''), IFNULL(i.pufferpanel_server_id, ''), IFNULL(i.game_version, ''), IFNULL(i.puffer_version_key, ''), IFNULL(i.created_at, ''), IFNULL(i.last_sync_at, ''), IFNULL(i.last_sync_added, 0), IFNULL(i.last_sync_updated, 0), IFNULL(i.last_sync_failed, 0),
             (SELECT COUNT(*) FROM mods m WHERE m.instance_id = i.id)
             FROM instances i WHERE i.id=?`, id).Scan(&inst.ID, &inst.Name, &inst.Loader, &inst.PufferpanelServerID, &inst.GameVersion, &inst.PufferVersionKey, &inst.CreatedAt, &inst.LastSyncAt, &inst.LastSyncAdded, &inst.LastSyncUpdated, &inst.LastSyncFailed, &inst.ModCount)
    if err != nil {
        return nil, err
    }
    return &inst, nil
}

// ListInstances returns all instances sorted by ID descending.
func ListInstances(db *sql.DB) ([]Instance, error) {
    rows, err := db.Query(`SELECT i.id, IFNULL(i.name, ''), IFNULL(i.loader, ''), IFNULL(i.pufferpanel_server_id, ''), IFNULL(i.game_version, ''), IFNULL(i.puffer_version_key, ''), IFNULL(i.created_at, ''), IFNULL(i.last_sync_at, ''), IFNULL(i.last_sync_added, 0), IFNULL(i.last_sync_updated, 0), IFNULL(i.last_sync_failed, 0), COUNT(m.id)
              FROM instances i LEFT JOIN mods m ON m.instance_id = i.id GROUP BY i.id ORDER BY i.id DESC`)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    out := []Instance{}
    for rows.Next() {
        var inst Instance
        if err := rows.Scan(&inst.ID, &inst.Name, &inst.Loader, &inst.PufferpanelServerID, &inst.GameVersion, &inst.PufferVersionKey, &inst.CreatedAt, &inst.LastSyncAt, &inst.LastSyncAdded, &inst.LastSyncUpdated, &inst.LastSyncFailed, &inst.ModCount); err != nil {
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

// SetModSyncState records the outcome of a mod sync attempt for an instance.
func SetModSyncState(db *sql.DB, instanceID int, slug, version, status string) error {
        _, err := db.Exec(`INSERT INTO mod_sync_state(instance_id, slug, last_checked_at, last_version, status) VALUES(?,?,?,?,?)
ON CONFLICT(instance_id, slug) DO UPDATE SET last_checked_at=excluded.last_checked_at, last_version=excluded.last_version, status=excluded.status`, instanceID, slug, time.Now().UTC(), version, status)
        return err
}

// ListModSyncStates returns recorded sync states for mods belonging to an instance.
func ListModSyncStates(db *sql.DB, instanceID int) ([]ModSyncState, error) {
        rows, err := db.Query(`SELECT slug, IFNULL(last_checked_at, ''), IFNULL(last_version, ''), IFNULL(status, '') FROM mod_sync_state WHERE instance_id=?`, instanceID)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        out := []ModSyncState{}
        for rows.Next() {
                var s ModSyncState
                if err := rows.Scan(&s.Slug, &s.LastChecked, &s.LastVersion, &s.Status); err != nil {
                        return nil, err
                }
                out = append(out, s)
        }
        if err := rows.Err(); err != nil {
                return nil, err
        }
        return out, nil
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

// InsertModUpdateQueued inserts or returns an existing mod update job by idempotency key.
// Returns (id, existed, error).
func InsertModUpdateQueued(db *sql.DB, modID int, fromVersion, toVersion, key string) (int, bool, error) {
    if key == "" {
        // fall back to non-unique insert when key is missing
        res, err := db.Exec(`INSERT INTO mod_updates(mod_id, from_version, to_version, status, idempotency_key) VALUES(?,?,?,?,?)`, modID, fromVersion, toVersion, "Queued", fmt.Sprintf("%d:%s", modID, toVersion))
        if err != nil {
            return 0, false, err
        }
        id64, _ := res.LastInsertId()
        return int(id64), false, nil
    }
    var existingID int
    if err := db.QueryRow(`SELECT id FROM mod_updates WHERE idempotency_key=?`, key).Scan(&existingID); err == nil {
        return existingID, true, nil
    }
    res, err := db.Exec(`INSERT INTO mod_updates(mod_id, from_version, to_version, status, idempotency_key) VALUES(?,?,?,?,?)`, modID, fromVersion, toVersion, "Queued", key)
    if err != nil {
        return 0, false, err
    }
    id64, _ := res.LastInsertId()
    return int(id64), false, nil
}

// MarkModUpdateStarted marks an update job as running and records the start time if not set.
func MarkModUpdateStarted(db *sql.DB, id int) error {
    _, err := db.Exec(`UPDATE mod_updates SET status='Running', started_at=COALESCE(started_at, CURRENT_TIMESTAMP), error=NULL WHERE id=?`, id)
    return err
}

// UpdateModUpdateStatus sets a transient status for an update job.
func UpdateModUpdateStatus(db *sql.DB, id int, status string) error {
    _, err := db.Exec(`UPDATE mod_updates SET status=? WHERE id=?`, status, id)
    return err
}

// MarkModUpdateFinished finalizes an update job with a terminal status and end time.
func MarkModUpdateFinished(db *sql.DB, id int, status, errMsg string) error {
    _, err := db.Exec(`UPDATE mod_updates SET status=?, ended_at=CURRENT_TIMESTAMP, error=? WHERE id=?`, status, errMsg, id)
    return err
}

// ListQueuedModUpdates returns IDs of queued mod update jobs.
func ListQueuedModUpdates(db *sql.DB) ([]int, error) {
    rows, err := db.Query(`SELECT id FROM mod_updates WHERE status='Queued' ORDER BY id ASC`)
    if err != nil { return nil, err }
    defer rows.Close()
    out := []int{}
    for rows.Next() {
        var id int
        if err := rows.Scan(&id); err != nil { return nil, err }
        out = append(out, id)
    }
    if err := rows.Err(); err != nil { return nil, err }
    return out, nil
}

// ResetRunningModUpdates moves running, unfinished updates back to queued (e.g., after crash).
func ResetRunningModUpdates(db *sql.DB) error {
    _, err := db.Exec(`UPDATE mod_updates SET status='Queued' WHERE status='Running' AND ended_at IS NULL`)
    return err
}

// LoaderTag stores Modrinth loader metadata.
type LoaderTag struct {
    ID    string
    Name  string
    Icon  string
    Types []string
}

// UpsertModrinthLoaders saves loader tags to the database.
func UpsertModrinthLoaders(db *sql.DB, tags []LoaderTag) error {
    if len(tags) == 0 { return nil }
    // Ensure table exists (in case migration hasn't run yet)
    if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS modrinth_loaders (
        id TEXT PRIMARY KEY,
        name TEXT,
        icon TEXT,
        types TEXT
    )`); err != nil { return err }
    tx, err := db.Begin()
    if err != nil { return err }
    defer tx.Rollback()
    stmt, err := tx.Prepare(`INSERT INTO modrinth_loaders(id, name, icon, types)
VALUES(?,?,?,?)
ON CONFLICT(id) DO UPDATE SET name=excluded.name, icon=excluded.icon, types=excluded.types`)
    if err != nil { return err }
    defer stmt.Close()
    for _, t := range tags {
        id := strings.ToLower(strings.TrimSpace(t.ID))
        if id == "" { continue }
        types := strings.Join(t.Types, ",")
        if _, err := stmt.Exec(id, t.Name, t.Icon, types); err != nil { return err }
    }
    return tx.Commit()
}

// LeaseModUpdate attempts to transition a queued job to running; returns true if lease obtained.
func LeaseModUpdate(db *sql.DB, id int) (bool, error) {
    res, err := db.Exec(`UPDATE mod_updates SET status='Running', started_at=COALESCE(started_at, CURRENT_TIMESTAMP) WHERE id=? AND status='Queued'`, id)
    if err != nil { return false, err }
    n, _ := res.RowsAffected()
    return n > 0, nil
}

// GetModUpdate returns mod_id and status for a mod update row.
type ModUpdateRow struct {
    ID int
    ModID int
    FromVersion string
    ToVersion string
    Status string
    StartedAt string
    EndedAt string
}

func GetModUpdate(db *sql.DB, id int) (*ModUpdateRow, error) {
    var mu ModUpdateRow
    err := db.QueryRow(`SELECT id, mod_id, IFNULL(from_version,''), IFNULL(to_version,''), IFNULL(status,''), IFNULL(started_at,''), IFNULL(ended_at,'') FROM mod_updates WHERE id=?`, id).
        Scan(&mu.ID, &mu.ModID, &mu.FromVersion, &mu.ToVersion, &mu.Status, &mu.StartedAt, &mu.EndedAt)
    if err != nil { return nil, err }
    return &mu, nil
}

// InsertEvent stores a mod activity log entry.
func InsertEvent(db *sql.DB, ev *ModEvent) error {
    var modID any
    if ev.ModID == nil {
        modID = nil
    } else {
        modID = *ev.ModID
    }
    res, err := db.Exec(`INSERT INTO mod_events(instance_id, mod_id, action, mod_name, from_version, to_version) VALUES(?,?,?,?,?,?)`, ev.InstanceID, modID, ev.Action, ev.ModName, ev.From, ev.To)
    if err != nil {
        return err
    }
    if id, err2 := res.LastInsertId(); err2 == nil {
        ev.ID = int(id)
    }
    return nil
}

// ListEvents returns recent mod events for an instance ordered by newest first.
func ListEvents(db *sql.DB, instanceID, limit int) ([]ModEvent, error) {
    if limit <= 0 || limit > 500 {
        limit = 100
    }
    rows, err := db.Query(`SELECT id, instance_id, mod_id, action, mod_name, IFNULL(from_version,''), IFNULL(to_version,''), IFNULL(created_at,'') FROM mod_events WHERE instance_id=? ORDER BY id DESC LIMIT ?`, instanceID, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    out := []ModEvent{}
    for rows.Next() {
        var ev ModEvent
        var modID sql.NullInt64
        if err := rows.Scan(&ev.ID, &ev.InstanceID, &modID, &ev.Action, &ev.ModName, &ev.From, &ev.To, &ev.CreatedAt); err != nil {
            return nil, err
        }
        if modID.Valid {
            id := int(modID.Int64)
            ev.ModID = &id
        }
        out = append(out, ev)
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }
    return out, nil
}

// GetAlias returns the canonical slug for a given alias/candidate within an instance.
func GetAlias(db *sql.DB, instanceID int, alias string) (string, bool, error) {
    var slug string
    err := db.QueryRow(`SELECT slug FROM slug_aliases WHERE instance_id=? AND alias=?`, instanceID, alias).Scan(&slug)
    if err == sql.ErrNoRows {
        return "", false, nil
    }
    if err != nil {
        return "", false, err
    }
    return slug, true, nil
}

// SetAlias upserts the alias mapping for an instance.
func SetAlias(db *sql.DB, instanceID int, alias, slug string) error {
    _, err := db.Exec(`INSERT INTO slug_aliases(instance_id, alias, slug) VALUES(?,?,?)
ON CONFLICT(instance_id, alias) DO UPDATE SET slug=excluded.slug`, instanceID, alias, slug)
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

// SyncJob represents a background instance sync job.
type SyncJob struct {
	ID         int
	InstanceID int
	ServerID   string
	Status     string
	Error      string
	Key        string
}

// ResetRunningSyncJobs resets running jobs back to queued on startup.
func ResetRunningSyncJobs(db *sql.DB) error {
	_, err := db.Exec(`UPDATE sync_jobs SET status='queued', started_at=NULL WHERE status='running'`)
	return err
}

// ListQueuedSyncJobs returns IDs of jobs awaiting processing.
func ListQueuedSyncJobs(db *sql.DB) ([]int, error) {
	rows, err := db.Query(`SELECT id FROM sync_jobs WHERE status='queued' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// InsertSyncJob enqueues a new sync job and returns its ID. If a job already
// exists for the given instance and key, the existing ID is returned with
// existed set to true.
func InsertSyncJob(db *sql.DB, instanceID int, serverID, key string) (id int, existed bool, err error) {
	err = db.QueryRow(`SELECT id FROM sync_jobs WHERE instance_id=? AND idempotency_key=?`, instanceID, key).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	if err != sql.ErrNoRows {
		return 0, false, err
	}
	res, err := db.Exec(`INSERT INTO sync_jobs(instance_id, server_id, status, idempotency_key) VALUES(?, ?, 'queued', ?)`, instanceID, serverID, key)
	if err != nil {
		return 0, false, err
	}
	lastID, err := res.LastInsertId()
	if err != nil {
		return 0, false, err
	}
	return int(lastID), false, nil
}

// GetSyncJob returns a sync job by ID.
func GetSyncJob(db *sql.DB, id int) (*SyncJob, error) {
	var j SyncJob
	err := db.QueryRow(`SELECT id, instance_id, server_id, IFNULL(status,''), IFNULL(error,''), IFNULL(idempotency_key,'') FROM sync_jobs WHERE id=?`, id).Scan(&j.ID, &j.InstanceID, &j.ServerID, &j.Status, &j.Error, &j.Key)
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// MarkSyncJobRunning sets a job to running.
func MarkSyncJobRunning(db *sql.DB, id int) error {
	_, err := db.Exec(`UPDATE sync_jobs SET status='running', started_at=CURRENT_TIMESTAMP WHERE id=?`, id)
	return err
}

// MarkSyncJobFinished updates a job to a terminal status.
func MarkSyncJobFinished(db *sql.DB, id int, status, errMsg string) error {
	_, err := db.Exec(`UPDATE sync_jobs SET status=?, error=?, finished_at=CURRENT_TIMESTAMP WHERE id=?`, status, errMsg, id)
	return err
}

// RequeueSyncJob resets a finished job back to queued.
func RequeueSyncJob(db *sql.DB, id int) error {
	_, err := db.Exec(`UPDATE sync_jobs SET status='queued', error='', started_at=NULL, finished_at=NULL WHERE id=?`, id)
	return err
}
