package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	dbpkg "modsentinel/internal/db"
	"modsentinel/internal/httpx"
	mr "modsentinel/internal/modrinth"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbpkg.Init(db); err != nil {
		t.Fatalf("init db: %v", err)
	}
	return db
}

type fakeModClient struct{}

func (fakeModClient) Project(ctx context.Context, slug string) (*mr.Project, error) {
	return &mr.Project{Title: "Fake", IconURL: ""}, nil
}

func (fakeModClient) Versions(ctx context.Context, slug, gameVersion, loader string) ([]mr.Version, error) {
	return []mr.Version{{
		ID:            "1",
		VersionNumber: "1.0",
		VersionType:   "release",
		DatePublished: time.Now(),
		Files:         []mr.VersionFile{{URL: "http://example.com"}},
	}}, nil
}

func TestCreateModHandler_EnforceLoader(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	inst := &dbpkg.Instance{Name: "A", Loader: "fabric", EnforceSameLoader: true}
	if err := dbpkg.InsertInstance(db, inst); err != nil {
		t.Fatalf("insert instance: %v", err)
	}

	h := createModHandler(db)

	payload := `{"url":"https://modrinth.com/mod/sodium","game_version":"1.20","loader":"forge","channel":"release","instance_id":` + strconv.Itoa(inst.ID) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/mods", strings.NewReader(payload))
	w := httptest.NewRecorder()

	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d", w.Code)
	}
	var errResp httpx.Error
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errResp.Message != "loader mismatch" {
		t.Fatalf("want loader mismatch, got %q", errResp.Message)
	}
}

func TestListModsHandler_ScopeAndCache(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	inst1 := &dbpkg.Instance{Name: "A", Loader: "fabric"}
	if err := dbpkg.InsertInstance(db, inst1); err != nil {
		t.Fatalf("insert inst1: %v", err)
	}
	inst2 := &dbpkg.Instance{Name: "B", Loader: "forge"}
	if err := dbpkg.InsertInstance(db, inst2); err != nil {
		t.Fatalf("insert inst2: %v", err)
	}

	if err := dbpkg.InsertMod(db, &dbpkg.Mod{Name: "M1", URL: "u1", InstanceID: inst1.ID}); err != nil {
		t.Fatalf("insert mod1: %v", err)
	}
	if err := dbpkg.InsertMod(db, &dbpkg.Mod{Name: "M2", URL: "u2", InstanceID: inst2.ID}); err != nil {
		t.Fatalf("insert mod2: %v", err)
	}

	h := listModsHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/mods?instance_id="+strconv.Itoa(inst1.ID), nil)
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "max-age=60" {
		t.Fatalf("cache-control %q", cc)
	}
	var mods []dbpkg.Mod
	if err := json.NewDecoder(w.Body).Decode(&mods); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(mods) != 1 || mods[0].InstanceID != inst1.ID {
		t.Fatalf("unexpected mods: %v", mods)
	}
}

func TestInstanceHandlers_CRUD(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	// Create with default enforcement (true)
	create := createInstanceHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/instances", strings.NewReader(`{"name":"A","loader":"fabric"}`))
	w := httptest.NewRecorder()
	create(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create status %d", w.Code)
	}
	var inst dbpkg.Instance
	if err := json.NewDecoder(w.Body).Decode(&inst); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if !inst.EnforceSameLoader {
		t.Fatalf("expected enforcement default true")
	}

	// Create with enforcement disabled
	req2 := httptest.NewRequest(http.MethodPost, "/api/instances", strings.NewReader(`{"name":"B","loader":"forge","enforce_same_loader":false}`))
	w2 := httptest.NewRecorder()
	create(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("create2 status %d", w2.Code)
	}
	var inst2 dbpkg.Instance
	if err := json.NewDecoder(w2.Body).Decode(&inst2); err != nil {
		t.Fatalf("decode create2: %v", err)
	}
	if inst2.EnforceSameLoader {
		t.Fatalf("expected enforcement false")
	}

	// Update second instance to enable enforcement
	update := updateInstanceHandler(db)
	payload := fmt.Sprintf(`{"name":"B2","loader":"forge","enforce_same_loader":true}`)
	req3 := httptest.NewRequest(http.MethodPut, "/api/instances/"+strconv.Itoa(inst2.ID), strings.NewReader(payload))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(inst2.ID))
	req3 = req3.WithContext(context.WithValue(req3.Context(), chi.RouteCtxKey, rctx))
	w3 := httptest.NewRecorder()
	update(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("update status %d", w3.Code)
	}
	var instUpd dbpkg.Instance
	if err := json.NewDecoder(w3.Body).Decode(&instUpd); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if !instUpd.EnforceSameLoader {
		t.Fatalf("expected enforcement true after update")
	}

	// Delete first instance
	deleteH := deleteInstanceHandler(db)
	req4 := httptest.NewRequest(http.MethodDelete, "/api/instances/"+strconv.Itoa(inst.ID), nil)
	rctx4 := chi.NewRouteContext()
	rctx4.URLParams.Add("id", strconv.Itoa(inst.ID))
	req4 = req4.WithContext(context.WithValue(req4.Context(), chi.RouteCtxKey, rctx4))
	w4 := httptest.NewRecorder()
	deleteH(w4, req4)
	if w4.Code != http.StatusNoContent {
		t.Fatalf("delete status %d", w4.Code)
	}
	if _, err := dbpkg.GetInstance(db, inst.ID); err == nil {
		t.Fatalf("instance not deleted")
	}
}

func TestCreateModHandler_WarningWithoutEnforcement(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	inst := &dbpkg.Instance{Name: "A", Loader: "fabric", EnforceSameLoader: false}
	if err := dbpkg.InsertInstance(db, inst); err != nil {
		t.Fatalf("insert instance: %v", err)
	}

	oldClient := modClient
	modClient = fakeModClient{}
	defer func() { modClient = oldClient }()

	h := createModHandler(db)
	payload := `{"url":"https://modrinth.com/mod/sodium","game_version":"1.20","loader":"forge","channel":"release","instance_id":` + strconv.Itoa(inst.ID) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/mods", strings.NewReader(payload))
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var resp struct {
		Mods    []dbpkg.Mod `json:"mods"`
		Warning string      `json:"warning"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Warning != "loader mismatch" {
		t.Fatalf("expected warning, got %q", resp.Warning)
	}
	if len(resp.Mods) != 1 || resp.Mods[0].InstanceID != inst.ID {
		t.Fatalf("unexpected mods: %v", resp.Mods)
	}
}
