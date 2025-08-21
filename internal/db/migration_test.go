package db

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestInitCreatesDefaultInstance(t *testing.T) {
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// Simulate pre-migration mods table without instances.
	if _, err := db.Exec(`CREATE TABLE mods (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, url TEXT NOT NULL, loader TEXT)`); err != nil {
		t.Fatalf("create mods table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO mods(name, url, loader) VALUES('A', 'u', 'fabric')`); err != nil {
		t.Fatalf("insert mod: %v", err)
	}

	if err := Init(db); err != nil {
		t.Fatalf("init: %v", err)
	}

	var id int
	var loader string
	if err := db.QueryRow(`SELECT id, loader FROM instances LIMIT 1`).Scan(&id, &loader); err != nil {
		t.Fatalf("select instance: %v", err)
	}
	if loader != "fabric" {
		t.Fatalf("expected loader fabric, got %s", loader)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM mods WHERE instance_id=?`, id).Scan(&count); err != nil {
		t.Fatalf("count mods: %v", err)
	}
	if count != 1 {
		t.Fatalf("mods not migrated: %d", count)
	}
}

func TestInitCreatesSecretsTable(t *testing.T) {
	db, err := sql.Open("sqlite", "file:memdb2?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := Init(db); err != nil {
		t.Fatalf("init: %v", err)
	}

	rows, err := db.Query(`SELECT name FROM pragma_table_info('secrets')`)
	if err != nil {
		t.Fatalf("pragma: %v", err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		cols[n] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	expected := []string{"id", "type", "value_enc", "key_id", "iv", "created_at", "updated_at"}
	for _, c := range expected {
		if !cols[c] {
			t.Fatalf("missing column %s", c)
		}
	}
}
