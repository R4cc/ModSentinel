package handlers

import (
    "bytes"
    "context"
    "testing"

    "github.com/rs/zerolog"
    "github.com/rs/zerolog/log"
    dbpkg "modsentinel/internal/db"
    pppkg "modsentinel/internal/pufferpanel"
)

// Test that queued/job sync skips mod resolution when loader is unknown and emits telemetry.
func TestSync_SkipsWhenLoaderRequired_EmitsTelemetry(t *testing.T) {
    db := setupDB(t)
    defer db.Close()
    seedLoaders()

    inst := createInstance(t, db, "SkipMe")
    // Ensure no loader preset
    if _, err := db.Exec(`UPDATE instances SET loader='', requires_loader=0 WHERE id=?`, inst.ID); err != nil { t.Fatalf("prep: %v", err) }

    // Stubs produce no loader evidence
    origGet := ppGetServer
    origDef := ppGetServerDefinition
    origDefRaw := ppGetServerDefinitionRaw
    origData := ppGetServerData
    defer func(){ ppGetServer = origGet; ppGetServerDefinition = origDef; ppGetServerDefinitionRaw = origDefRaw; ppGetServerData = origData }()
    ppGetServer = func(_ context.Context, id string) (*pppkg.ServerDetail, error) { var d pppkg.ServerDetail; d.ID = id; d.Environment.Type = "java"; return &d, nil }
    ppGetServerDefinition = func(_ context.Context, _ string) (*pppkg.ServerDefinition, error) { return &pppkg.ServerDefinition{Data: map[string]pppkg.Variable{}}, nil }
    ppGetServerDefinitionRaw = func(_ context.Context, _ string) (map[string]any, error) { return map[string]any{"environment": map[string]any{"display": "Minecraft Java"}}, nil }
    ppGetServerData = func(_ context.Context, _ string) (*pppkg.ServerData, error) { return &pppkg.ServerData{Data: map[string]pppkg.ValueWrapper{}}, nil }

    // Capture logs to assert telemetry event
    var buf bytes.Buffer
    prev := log.Logger
    log.Logger = log.Output(&buf)
    t.Cleanup(func(){ log.Logger = prev })

    jw := &jobWriter{}
    performSync(context.Background(), jw, nil, db, inst, "1", &jobProgress{}, nil)

    // jobWriter should not write a status when skipping
    if jw.status != 0 {
        t.Fatalf("expected no HTTP status written, got %d", jw.status)
    }
    // DB should reflect loader required
    got, err := dbpkg.GetInstance(db, inst.ID)
    if err != nil { t.Fatalf("GetInstance: %v", err) }
    if !got.RequiresLoader {
        t.Fatalf("expected requires_loader=true")
    }
    // Telemetry logged
    if !bytes.Contains(buf.Bytes(), []byte("\"event\":\"sync_skip\"")) {
        t.Fatalf("expected sync_skip telemetry, got logs: %s", buf.String())
    }
}

