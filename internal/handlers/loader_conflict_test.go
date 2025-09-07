package handlers

import (
    "context"
    "net/http/httptest"
    "testing"

    dbpkg "modsentinel/internal/db"
    pppkg "modsentinel/internal/pufferpanel"
)

// Test that contradictory loader evidence results in unknown and no loader mutation.
func TestLoaderDetect_ConflictingEvidence_IsUnknown_NoMutation(t *testing.T) {
    db := setupDB(t)
    defer db.Close()
    seedLoaders()

    // Pre-set a loader to verify it is not mutated on unknown detection
    inst := createInstance(t, db, "Conflicting")
    inst.Loader = "forge"
    if _, err := db.Exec(`UPDATE instances SET loader='forge', requires_loader=0 WHERE id=?`, inst.ID); err != nil {
        t.Fatalf("prep inst: %v", err)
    }

    // Stubs: conflicting tokens between display (neo forge) and install (fabric)
    origGet := ppGetServer
    origDef := ppGetServerDefinition
    origDefRaw := ppGetServerDefinitionRaw
    origData := ppGetServerData
    defer func(){ ppGetServer = origGet; ppGetServerDefinition = origDef; ppGetServerDefinitionRaw = origDefRaw; ppGetServerData = origData }()
    ppGetServer = func(_ context.Context, id string) (*pppkg.ServerDetail, error) { var d pppkg.ServerDetail; d.ID = id; d.Environment.Type = "java"; return &d, nil }
    ppGetServerDefinition = func(_ context.Context, _ string) (*pppkg.ServerDefinition, error) {
        return &pppkg.ServerDefinition{Data: map[string]pppkg.Variable{}}, nil
    }
    ppGetServerDefinitionRaw = func(_ context.Context, _ string) (map[string]any, error) {
        return map[string]any{
            "display": "Neo Forge Server", // neoforge token
            "install": []any{
                map[string]any{"type": "fabricdl"}, // fabric token
            },
        }, nil
    }
    ppGetServerData = func(_ context.Context, _ string) (*pppkg.ServerData, error) { return &pppkg.ServerData{Data: map[string]pppkg.ValueWrapper{}}, nil }

    rr := httptest.NewRecorder()
    req := httptest.NewRequest("POST", "/api/instances/1/sync", nil)
    performSync(context.Background(), rr, req, db, inst, "1", &jobProgress{}, nil)

    if rr.Code != 409 { t.Fatalf("expected 409, got %d", rr.Code) }
    got, err := dbpkg.GetInstance(db, inst.ID)
    if err != nil { t.Fatalf("GetInstance: %v", err) }
    if !got.RequiresLoader {
        t.Fatalf("expected requires_loader=true, got false")
    }
    if got.Loader != "forge" {
        t.Fatalf("loader mutated: want 'forge', got %q", got.Loader)
    }
}

