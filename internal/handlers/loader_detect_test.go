package handlers

import (
    "context"
    "net/http/httptest"
    "testing"
    "time"

    dbpkg "modsentinel/internal/db"
    pppkg "modsentinel/internal/pufferpanel"
)

// seedLoaders populates the Modrinth loader cache for tests.
func seedLoaders() {
    modrinthLoadersMu.Lock()
    modrinthLoadersCache = []metaLoaderOut{
        {ID: "fabric", Name: "Fabric"},
        {ID: "forge", Name: "Forge"},
        {ID: "quilt", Name: "Quilt"},
        {ID: "neoforge", Name: "NeoForge"},
    }
    modrinthLoadersExpiry = time.Now().Add(1 * time.Hour)
    modrinthLoadersMu.Unlock()
}

func TestLoaderDetect_Fabric_FromInstallType(t *testing.T) {
    db := setupDB(t)
    defer db.Close()
    seedLoaders()

    inst := createInstance(t, db, "Inst A")
    inst.Loader = ""
    if _, err := db.Exec(`UPDATE instances SET loader='', requires_loader=1 WHERE id=?`, inst.ID); err != nil {
        t.Fatalf("prep inst: %v", err)
    }

    // Stub PufferPanel deps
    origGet := ppGetServer
    origList := ppListPath
    origDef := ppGetServerDefinition
    origDefRaw := ppGetServerDefinitionRaw
    origData := ppGetServerData
    defer func(){ ppGetServer = origGet; ppListPath = origList; ppGetServerDefinition = origDef; ppGetServerDefinitionRaw = origDefRaw; ppGetServerData = origData }()
    ppGetServer = func(_ context.Context, id string) (*pppkg.ServerDetail, error) {
        var d pppkg.ServerDetail
        d.ID = id
        d.Environment.Type = "java"
        return &d, nil
    }
    ppListPath = func(_ context.Context, _ string, _ string) ([]pppkg.FileEntry, error) { return nil, nil }
    ppGetServerDefinition = func(_ context.Context, _ string) (*pppkg.ServerDefinition, error) {
        return &pppkg.ServerDefinition{Data: map[string]pppkg.Variable{}}, nil
    }
    ppGetServerDefinitionRaw = func(_ context.Context, _ string) (map[string]any, error) {
        return map[string]any{
            "environment": map[string]any{"display": "Minecraft Java"},
            "install": []any{
                map[string]any{"type": "fabricdl"},
            },
        }, nil
    }
    ppGetServerData = func(_ context.Context, _ string) (*pppkg.ServerData, error) { return &pppkg.ServerData{Data: map[string]pppkg.ValueWrapper{}}, nil }

    rr := httptest.NewRecorder()
    req := httptest.NewRequest("POST", "/api/instances/1/sync", nil)
    performSync(context.Background(), rr, req, db, inst, "1", &jobProgress{}, nil)

    if rr.Code >= 400 {
        t.Fatalf("unexpected http status %d", rr.Code)
    }
    got, err := dbpkg.GetInstance(db, inst.ID)
    if err != nil { t.Fatalf("GetInstance: %v", err) }
    if got.Loader != "fabric" || got.RequiresLoader {
        t.Fatalf("expected fabric and requires_loader=false, got loader=%q requires=%v", got.Loader, got.RequiresLoader)
    }
}

func TestLoaderDetect_Display_Variants(t *testing.T) {
    cases := []struct{ display, want string }{
        {"Minecraft Forge Server", "forge"},
        {"Quilt Server", "quilt"},
        {"Neo Forge Server", "neoforge"},
    }
    for _, tc := range cases {
        t.Run(tc.want, func(t *testing.T) {
            db := setupDB(t)
            defer db.Close()
            seedLoaders()
            inst := createInstance(t, db, "Inst B")
            inst.Loader = ""
            if _, err := db.Exec(`UPDATE instances SET loader='', requires_loader=1 WHERE id=?`, inst.ID); err != nil {
                t.Fatalf("prep inst: %v", err)
            }
            // Stubs
            origGet := ppGetServer
            origList := ppListPath
            origDef := ppGetServerDefinition
            origDefRaw := ppGetServerDefinitionRaw
            origData := ppGetServerData
            defer func(){ ppGetServer = origGet; ppListPath = origList; ppGetServerDefinition = origDef; ppGetServerDefinitionRaw = origDefRaw; ppGetServerData = origData }()
            ppGetServer = func(_ context.Context, id string) (*pppkg.ServerDetail, error) { var d pppkg.ServerDetail; d.ID = id; d.Environment.Type = "java"; return &d, nil }
            ppListPath = func(_ context.Context, _ string, _ string) ([]pppkg.FileEntry, error) { return nil, nil }
            ppGetServerDefinition = func(_ context.Context, _ string) (*pppkg.ServerDefinition, error) { return &pppkg.ServerDefinition{Data: map[string]pppkg.Variable{}}, nil }
            ppGetServerDefinitionRaw = func(_ context.Context, _ string) (map[string]any, error) {
                return map[string]any{ "environment": map[string]any{"display": tc.display} }, nil
            }
            ppGetServerData = func(_ context.Context, _ string) (*pppkg.ServerData, error) { return &pppkg.ServerData{Data: map[string]pppkg.ValueWrapper{}}, nil }

            rr := httptest.NewRecorder()
            req := httptest.NewRequest("POST", "/api/instances/1/sync", nil)
            performSync(context.Background(), rr, req, db, inst, "1", &jobProgress{}, nil)

            if rr.Code >= 400 {
                t.Fatalf("unexpected http status %d", rr.Code)
            }
            got, err := dbpkg.GetInstance(db, inst.ID)
            if err != nil { t.Fatalf("GetInstance: %v", err) }
            if got.Loader != tc.want || got.RequiresLoader {
                t.Fatalf("expected %s and requires_loader=false, got loader=%q requires=%v", tc.want, got.Loader, got.RequiresLoader)
            }
        })
    }
}

func TestLoaderDetect_Unmatched_ReturnsLoaderRequired(t *testing.T) {
    db := setupDB(t)
    defer db.Close()
    seedLoaders()
    inst := createInstance(t, db, "Inst C")
    inst.Loader = ""
    if _, err := db.Exec(`UPDATE instances SET loader='', requires_loader=0 WHERE id=?`, inst.ID); err != nil { t.Fatalf("prep inst: %v", err) }

    // Stubs
    origGet := ppGetServer
    origList := ppListPath
    origDef := ppGetServerDefinition
    origDefRaw := ppGetServerDefinitionRaw
    origData := ppGetServerData
    defer func(){ ppGetServer = origGet; ppListPath = origList; ppGetServerDefinition = origDef; ppGetServerDefinitionRaw = origDefRaw; ppGetServerData = origData }()
    ppGetServer = func(_ context.Context, id string) (*pppkg.ServerDetail, error) { var d pppkg.ServerDetail; d.ID = id; d.Environment.Type = "java"; return &d, nil }
    ppListPath = func(_ context.Context, _ string, _ string) ([]pppkg.FileEntry, error) { return nil, nil }
    ppGetServerDefinition = func(_ context.Context, _ string) (*pppkg.ServerDefinition, error) { return &pppkg.ServerDefinition{Data: map[string]pppkg.Variable{}}, nil }
    ppGetServerDefinitionRaw = func(_ context.Context, _ string) (map[string]any, error) {
        return map[string]any{ "environment": map[string]any{"display": "Minecraft Java"} }, nil
    }
    ppGetServerData = func(_ context.Context, _ string) (*pppkg.ServerData, error) { return &pppkg.ServerData{Data: map[string]pppkg.ValueWrapper{}}, nil }

    rr := httptest.NewRecorder()
    req := httptest.NewRequest("POST", "/api/instances/1/sync", nil)
    performSync(context.Background(), rr, req, db, inst, "1", &jobProgress{}, nil)

    if rr.Code != 409 { t.Fatalf("expected 409, got %d", rr.Code) }
    got, err := dbpkg.GetInstance(db, inst.ID)
    if err != nil { t.Fatalf("GetInstance: %v", err) }
    if !got.RequiresLoader || got.Loader != "" {
        t.Fatalf("expected requires_loader=true and empty loader, got loader=%q requires=%v", got.Loader, got.RequiresLoader)
    }
}

