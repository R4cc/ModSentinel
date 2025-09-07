package handlers

import (
    "bytes"
    "context"
    "database/sql"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strconv"
    "testing"

    dbpkg "modsentinel/internal/db"
    "github.com/go-chi/chi/v5"
)

func mustSetupInstanceWithMod(t *testing.T, db *sql.DB, requireLoader bool) (*dbpkg.Instance, *dbpkg.Mod) {
    t.Helper()
    inst := createInstance(t, db, "Inst API")
    if requireLoader {
        if _, err := db.Exec(`UPDATE instances SET requires_loader=1 WHERE id=?`, inst.ID); err != nil { t.Fatalf("prep: %v", err) }
    }
    m := &dbpkg.Mod{InstanceID: inst.ID, Name: "A", URL: "https://modrinth.com/mod/sodium"}
    if err := dbpkg.InsertMod(db, m); err != nil { t.Fatalf("insert mod: %v", err) }
    return inst, m
}

func TestModActions_BlockWhenLoaderRequired(t *testing.T) {
    db := setupDB(t)
    defer db.Close()
    seedLoaders()
    inst, mod := mustSetupInstanceWithMod(t, db, true)

    // createModHandler
    {
        body := map[string]any{"mod": map[string]any{"instance_id": inst.ID, "name": "B", "url": "https://modrinth.com/mod/lithium"}}
        b, _ := json.Marshal(body)
        rr := httptest.NewRecorder()
        req := httptest.NewRequest(http.MethodPost, "/api/mods", bytes.NewReader(b))
        createModHandler(db).ServeHTTP(rr, req)
        if rr.Code != 409 { t.Fatalf("create: want 409, got %d", rr.Code) }
    }
    // updateModHandler
    {
        payload := map[string]any{"id": mod.ID, "instance_id": inst.ID, "name": mod.Name, "url": mod.URL}
        b, _ := json.Marshal(payload)
        rr := httptest.NewRecorder()
        req := httptest.NewRequest(http.MethodPut, "/api/mods/"+strconv.Itoa(mod.ID), bytes.NewReader(b))
        req = muxParam(req, "id", strconv.Itoa(mod.ID))
        updateModHandler(db).ServeHTTP(rr, req)
        if rr.Code != 409 { t.Fatalf("update: want 409, got %d", rr.Code) }
    }
    // deleteModHandler
    {
        rr := httptest.NewRecorder()
        req := httptest.NewRequest(http.MethodDelete, "/api/mods/"+strconv.Itoa(mod.ID)+"?instance_id="+strconv.Itoa(inst.ID), nil)
        req = muxParam(req, "id", strconv.Itoa(mod.ID))
        deleteModHandler(db).ServeHTTP(rr, req)
        if rr.Code != 409 { t.Fatalf("delete: want 409, got %d", rr.Code) }
    }
    // checkModHandler
    {
        rr := httptest.NewRecorder()
        req := httptest.NewRequest(http.MethodGet, "/api/mods/"+strconv.Itoa(mod.ID)+"/check", nil)
        req = muxParam(req, "id", strconv.Itoa(mod.ID))
        checkModHandler(db).ServeHTTP(rr, req)
        if rr.Code != 409 { t.Fatalf("check: want 409, got %d", rr.Code) }
    }
}

// muxParam adds a chi URL param to a request for handler testing.
func muxParam(r *http.Request, key, val string) *http.Request {
    rctx := chi.NewRouteContext()
    rctx.URLParams.Add(key, val)
    return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}
