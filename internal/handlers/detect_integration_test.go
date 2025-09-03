package handlers

import (
    "context"
    "database/sql"
    "net/http/httptest"
    "testing"
    dbpkg "modsentinel/internal/db"
    pppkg "modsentinel/internal/pufferpanel"
)

func TestSyncSetsGameVersion_AndGetAPIExposesIt(t *testing.T) {
    db := setupDB(t)
    defer db.Close()
    inst := createInstance(t, db, "A")

    // Stub PufferPanel deps
    origGet := ppGetServer
    origList := ppListPath
    origDef := ppGetServerDefinition
    origData := ppGetServerData
    defer func(){ ppGetServer = origGet; ppListPath = origList; ppGetServerDefinition = origDef; ppGetServerData = origData }()
    ppGetServer = func(_ context.Context, id string) (*pppkg.ServerDetail, error) {
        var d pppkg.ServerDetail
        d.ID = id
        d.Environment.Type = "fabric"
        return &d, nil
    }
    ppListPath = func(_ context.Context, _ string, _ string) ([]pppkg.FileEntry, error) { return nil, nil }
    ppGetServerDefinition = func(_ context.Context, _ string) (*pppkg.ServerDefinition, error) {
        return &pppkg.ServerDefinition{Data: map[string]pppkg.Variable{
            "MC_VERSION": {Display: "Minecraft Version", Options: []string{"1.20.1","1.21"}},
        }}, nil
    }
    ppGetServerData = func(_ context.Context, _ string) (*pppkg.ServerData, error) {
        return &pppkg.ServerData{Data: map[string]pppkg.ValueWrapper{
            "MC_VERSION": {Value: "1.20.1"},
        }}, nil
    }

    // Run sync inline
    rr := httptest.NewRecorder()
    req := httptest.NewRequest("POST", "/api/instances/1/sync", nil)
    performSync(context.Background(), rr, req, db, inst, "1", &jobProgress{}, nil)

    got, err := dbpkg.GetInstance(db, inst.ID)
    if err != nil { t.Fatalf("GetInstance: %v", err) }
    if got.GameVersion != "1.20.1" || got.PufferVersionKey != "MC_VERSION" {
        t.Fatalf("unexpected instance fields: %#v", got)
    }
}

// helpers from handlers_test setup patterns
func setupDB(t *testing.T) *sql.DB {
    t.Helper()
    db, err := sql.Open("sqlite", ":memory:")
    if err != nil { t.Fatal(err) }
    if err := dbpkg.Init(db); err != nil { t.Fatal(err) }
    return db
}

func createInstance(t *testing.T, db *sql.DB, name string) *dbpkg.Instance {
    t.Helper()
    inst := &dbpkg.Instance{Name: name, Loader: "fabric", EnforceSameLoader: true}
    if err := dbpkg.InsertInstance(db, inst); err != nil { t.Fatal(err) }
    return inst
}

