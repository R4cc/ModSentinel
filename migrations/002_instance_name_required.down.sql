-- Revert instances.name constraint
PRAGMA foreign_keys=OFF;
CREATE TABLE instances_old (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT,
    loader TEXT,
    enforce_same_loader INTEGER DEFAULT 1,
    pufferpanel_server_id TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_sync_at DATETIME,
    last_sync_added INTEGER DEFAULT 0,
    last_sync_updated INTEGER DEFAULT 0,
    last_sync_failed INTEGER DEFAULT 0
);
INSERT INTO instances_old(id, name, loader, enforce_same_loader, pufferpanel_server_id, created_at, last_sync_at, last_sync_added, last_sync_updated, last_sync_failed)
    SELECT id, name, loader, enforce_same_loader, pufferpanel_server_id, created_at, last_sync_at, last_sync_added, last_sync_updated, last_sync_failed FROM instances;
DROP TABLE instances;
ALTER TABLE instances_old RENAME TO instances;
PRAGMA foreign_keys=ON;
