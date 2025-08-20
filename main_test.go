package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := initDB(db); err != nil {
		t.Fatalf("init db: %v", err)
	}
	inst := &Instance{Name: "Test", Loader: "fabric", EnforceSameLoader: true}
	if err := insertInstance(db, inst); err != nil {
		t.Fatalf("insert instance: %v", err)
	}
	return db
}

func TestParseModrinthSlug(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"mod", "https://modrinth.com/mod/sodium", "sodium", false},
		{"plugin", "https://modrinth.com/plugin/spark", "spark", false},
		{"datapack", "https://modrinth.com/datapack/data", "data", false},
		{"resourcepack", "https://modrinth.com/resourcepack/pack", "pack", false},
		{"no slug", "https://modrinth.com/mod", "", true},
		{"invalid url", ":", "", true},
		{"no category", "https://example.com/foo/bar", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseModrinthSlug(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestResolveDBPath(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.db")
	if err := os.WriteFile(file, []byte{}, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"directory", dir, filepath.Join(dir, "mods.db")},
		{"file", file, file},
		{"nonexistent", filepath.Join(dir, "no.db"), filepath.Join(dir, "no.db")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveDBPath(tt.input)
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestDatabaseOperations(t *testing.T) {
	tests := []struct {
		name    string
		run     func(db *sql.DB) error
		wantErr bool
	}{
		{
			name: "insert",
			run: func(db *sql.DB) error {
				m := &Mod{Name: "A", URL: "u", InstanceID: 1}
				if err := insertMod(db, m); err != nil {
					return err
				}
				mods, err := listMods(db, 1)
				if err != nil {
					return err
				}
				if len(mods) != 1 || mods[0].Name != "A" {
					return fmt.Errorf("unexpected mods: %v", mods)
				}
				return nil
			},
		},
		{
			name: "insert error",
			run: func(db *sql.DB) error {
				db.Close()
				return insertMod(db, &Mod{Name: "A", URL: "u"})
			},
			wantErr: true,
		},
		{
			name: "update",
			run: func(db *sql.DB) error {
				m := &Mod{Name: "Old", URL: "u", InstanceID: 1}
				if err := insertMod(db, m); err != nil {
					return err
				}
				m.Name = "New"
				if err := updateMod(db, m); err != nil {
					return err
				}
				mods, err := listMods(db, 1)
				if err != nil {
					return err
				}
				if mods[0].Name != "New" {
					return fmt.Errorf("expected name New, got %s", mods[0].Name)
				}
				return nil
			},
		},
		{
			name: "update error",
			run: func(db *sql.DB) error {
				m := &Mod{ID: 1}
				db.Close()
				return updateMod(db, m)
			},
			wantErr: true,
		},
		{
			name: "delete",
			run: func(db *sql.DB) error {
				m := &Mod{Name: "Del", URL: "u", InstanceID: 1}
				if err := insertMod(db, m); err != nil {
					return err
				}
				if err := deleteMod(db, m.ID); err != nil {
					return err
				}
				mods, err := listMods(db, 1)
				if err != nil {
					return err
				}
				if len(mods) != 0 {
					return fmt.Errorf("mods not deleted")
				}
				return nil
			},
		},
		{
			name: "delete error",
			run: func(db *sql.DB) error {
				db.Close()
				return deleteMod(db, 1)
			},
			wantErr: true,
		},
		{
			name: "list empty",
			run: func(db *sql.DB) error {
				mods, err := listMods(db, 1)
				if err != nil {
					return err
				}
				if len(mods) != 0 {
					return fmt.Errorf("expected no mods")
				}
				return nil
			},
		},
		{
			name: "list error",
			run: func(db *sql.DB) error {
				db.Close()
				_, err := listMods(db, 1)
				return err
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openTestDB(t)
			defer db.Close()
			err := tt.run(db)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
