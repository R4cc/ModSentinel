-- Drop enforce_same_loader from instances while preserving other columns and data
PRAGMA foreign_keys=OFF;
CREATE TABLE instances_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL CHECK(length(name) <= 128 AND length(trim(name)) > 0),
    loader TEXT,
    pufferpanel_server_id TEXT,
    game_version TEXT,
    puffer_version_key TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_sync_at DATETIME,
    last_sync_added INTEGER DEFAULT 0,
    last_sync_updated INTEGER DEFAULT 0,
    last_sync_failed INTEGER DEFAULT 0
);
INSERT INTO instances_new(
    id, name, loader, pufferpanel_server_id, game_version, puffer_version_key, created_at, last_sync_at, last_sync_added, last_sync_updated, last_sync_failed
)
SELECT 
    id, name, loader, pufferpanel_server_id, IFNULL(game_version,''), IFNULL(puffer_version_key,''), created_at, last_sync_at, last_sync_added, last_sync_updated, last_sync_failed
FROM instances;
DROP TABLE instances;
ALTER TABLE instances_new RENAME TO instances;
PRAGMA foreign_keys=ON;
-- Recreate helpful index
CREATE INDEX IF NOT EXISTS instances_game_version_idx ON instances(game_version);

