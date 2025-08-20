package main

import (
	"database/sql"

	dbpkg "modsentinel/internal/db"
)

type Mod = dbpkg.Mod
type Instance = dbpkg.Instance

func initDB(db *sql.DB) error { return dbpkg.Init(db) }

func insertMod(db *sql.DB, m *Mod) error { return dbpkg.InsertMod(db, m) }

func updateMod(db *sql.DB, m *Mod) error { return dbpkg.UpdateMod(db, m) }

func deleteMod(db *sql.DB, id int) error { return dbpkg.DeleteMod(db, id) }

func insertInstance(db *sql.DB, inst *Instance) error { return dbpkg.InsertInstance(db, inst) }

func listMods(db *sql.DB, instanceID int) ([]Mod, error) { return dbpkg.ListMods(db, instanceID) }
