-- Add NOT NULL and non-blank constraint to instances.name
PRAGMA foreign_keys=OFF;
UPDATE instances SET name = 'instance-' || id WHERE name IS NULL OR trim(name) = '';
CREATE TABLE instances_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL CHECK(length(name) <= 128 AND length(trim(name)) > 0),
    loader TEXT,
    enforce_same_loader INTEGER DEFAULT 1,
    pufferpanel_server_id TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_sync_at DATETIME,
    last_sync_added INTEGER DEFAULT 0,
    last_sync_updated INTEGER DEFAULT 0,
    last_sync_failed INTEGER DEFAULT 0
);
INSERT INTO instances_new(id, name, loader, enforce_same_loader, pufferpanel_server_id, created_at, last_sync_at, last_sync_added, last_sync_updated, last_sync_failed)
    SELECT id, name, loader, enforce_same_loader, pufferpanel_server_id, created_at, last_sync_at, last_sync_added, last_sync_updated, last_sync_failed FROM instances;
DROP TABLE instances;
ALTER TABLE instances_new RENAME TO instances;
PRAGMA foreign_keys=ON;
