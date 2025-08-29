package db

import (
    "database/sql"
    "testing"
)

func TestSetModSyncState(t *testing.T) {
    db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
    if err != nil {
        t.Fatalf("open db: %v", err)
    }
    defer db.Close()
    if err := Init(db); err != nil {
        t.Fatalf("init: %v", err)
    }
    if err := Migrate(db); err != nil {
        t.Fatalf("migrate: %v", err)
    }
    inst := &Instance{Name: "i"}
    if err := InsertInstance(db, inst); err != nil {
        t.Fatalf("insert inst: %v", err)
    }
    if err := SetModSyncState(db, inst.ID, "slug", "1.0.0", "failed"); err != nil {
        t.Fatalf("set state: %v", err)
    }
    states, err := ListModSyncStates(db, inst.ID)
    if err != nil {
        t.Fatalf("list states: %v", err)
    }
    if len(states) != 1 {
        t.Fatalf("got %d states want 1", len(states))
    }
    if states[0].Status != "failed" || states[0].LastVersion != "1.0.0" || states[0].Slug != "slug" {
        t.Fatalf("unexpected state: %#v", states[0])
    }
    // Update
    if err := SetModSyncState(db, inst.ID, "slug", "1.1.0", "succeeded"); err != nil {
        t.Fatalf("set state2: %v", err)
    }
    states, err = ListModSyncStates(db, inst.ID)
    if err != nil {
        t.Fatalf("list states2: %v", err)
    }
    if len(states) != 1 {
        t.Fatalf("got %d states want 1", len(states))
    }
    if states[0].Status != "succeeded" || states[0].LastVersion != "1.1.0" {
        t.Fatalf("unexpected state after update: %#v", states[0])
    }
    if states[0].LastChecked == "" {
        t.Fatalf("expected last_checked to be set")
    }
}

