package main

import (
	"database/sql"

	dbpkg "modsentinel/internal/db"
)

type Mod = dbpkg.Mod

func initDB(db *sql.DB) error { return dbpkg.Init(db) }

func insertMod(db *sql.DB, m *Mod) error { return dbpkg.InsertMod(db, m) }

func updateMod(db *sql.DB, m *Mod) error { return dbpkg.UpdateMod(db, m) }

func deleteMod(db *sql.DB, id int) error { return dbpkg.DeleteMod(db, id) }

func listMods(db *sql.DB) ([]Mod, error) { return dbpkg.ListMods(db) }
