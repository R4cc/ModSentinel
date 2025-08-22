package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	dbpkg "modsentinel/internal/db"
	"modsentinel/internal/httpx"
	mr "modsentinel/internal/modrinth"
	pppkg "modsentinel/internal/pufferpanel"
	"modsentinel/internal/secrets"
	tokenpkg "modsentinel/internal/token"

	_ "modernc.org/sqlite"
)

//go:embed testdata/**
var testFS embed.FS

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

func (fakeModClient) Search(ctx context.Context, query string) (*mr.SearchResult, error) {
	return &mr.SearchResult{Hits: []struct {
		ProjectID string `json:"project_id"`
		Slug      string `json:"slug"`
		Title     string `json:"title"`
	}{{ProjectID: "1", Slug: query, Title: "Fake"}}}, nil
}

type matchClient struct{}

func (matchClient) Project(ctx context.Context, slug string) (*mr.Project, error) {
	return &mr.Project{Title: "Sodium", IconURL: ""}, nil
}

func (matchClient) Versions(ctx context.Context, slug, gameVersion, loader string) ([]mr.Version, error) {
	return []mr.Version{{
		ID:            "1",
		VersionNumber: "1.0",
		VersionType:   "release",
		GameVersions:  []string{"1.20"},
		Loaders:       []string{"fabric"},
		Files:         []mr.VersionFile{{URL: "http://example.com"}},
	}}, nil
}

type errClient struct{}

func (errClient) Project(ctx context.Context, slug string) (*mr.Project, error) {
	return nil, &mr.Error{Status: http.StatusUnauthorized}
}

func (errClient) Versions(ctx context.Context, slug, gameVersion, loader string) ([]mr.Version, error) {
	return nil, &mr.Error{Status: http.StatusUnauthorized}
}

func (errClient) Search(ctx context.Context, query string) (*mr.SearchResult, error) {
	return nil, &mr.Error{Status: http.StatusUnauthorized}
}

func (matchClient) Search(ctx context.Context, query string) (*mr.SearchResult, error) {
	return &mr.SearchResult{Hits: []struct {
		ProjectID string `json:"project_id"`
		Slug      string `json:"slug"`
		Title     string `json:"title"`
	}{{ProjectID: "1", Slug: "sodium", Title: "Sodium"}}}, nil
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

	// Create with enforcement disabled and PufferPanel linkage
	req2 := httptest.NewRequest(http.MethodPost, "/api/instances", strings.NewReader(`{"name":"B","loader":"forge","enforce_same_loader":false,"pufferpanel_server_id":"srv1"}`))
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
	if inst2.PufferpanelServerID != "srv1" {
		t.Fatalf("expected server id persisted")
	}

	// Attempt to change loader should fail
	update := updateInstanceHandler(db)
	badPayload := fmt.Sprintf(`{"loader":"fabric"}`)
	reqBad := httptest.NewRequest(http.MethodPut, "/api/instances/"+strconv.Itoa(inst2.ID), strings.NewReader(badPayload))
	rctxBad := chi.NewRouteContext()
	rctxBad.URLParams.Add("id", strconv.Itoa(inst2.ID))
	reqBad = reqBad.WithContext(context.WithValue(reqBad.Context(), chi.RouteCtxKey, rctxBad))
	wBad := httptest.NewRecorder()
	update(wBad, reqBad)
	if wBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when changing loader, got %d", wBad.Code)
	}

	// Update second instance to enable enforcement
	payload := fmt.Sprintf(`{"name":"B2","enforce_same_loader":true}`)
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

func TestSyncHandler_ScansMods(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	// mock pufferpanel server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case r.URL.Path == "/api/servers/1":
			fmt.Fprint(w, `{"id":"1","name":"Srv","environment":{"type":"fabric"}}`)
		case r.URL.Path == "/api/servers/1/files/list" && r.URL.Query().Get("path") == "mods":
			http.NotFound(w, r)
		case r.URL.Path == "/api/servers/1/files/list" && r.URL.Query().Get("path") == "plugins":
			fmt.Fprint(w, `[{"name":"mod.jar","is_dir":false},{"name":"other.txt","is_dir":false}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	initSecrets(t, db)
	if err := pppkg.Set(pppkg.Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("set creds: %v", err)
	}
	inst := dbpkg.Instance{Name: "", Loader: "", EnforceSameLoader: true}
	if err := dbpkg.InsertInstance(db, &inst); err != nil {
		t.Fatalf("insert inst: %v", err)
	}
	h := syncHandler(db)
	body := fmt.Sprintf(`{"serverId":"1","instanceId":%d}`, inst.ID)
	req := httptest.NewRequest(http.MethodPost, "/api/pufferpanel/sync", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var resp struct {
		Instance  dbpkg.Instance `json:"instance"`
		Unmatched []string       `json:"unmatched"`
		Mods      []dbpkg.Mod    `json:"mods"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Instance.Name != "Srv" || resp.Instance.Loader != "fabric" || resp.Instance.PufferpanelServerID != "1" {
		t.Fatalf("unexpected instance %+v", resp.Instance)
	}
	if len(resp.Unmatched) != 1 || resp.Unmatched[0] != "mod.jar" {
		t.Fatalf("unexpected unmatched %v", resp.Unmatched)
	}
}

func TestSyncHandler_MatchesMods(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case r.URL.Path == "/api/servers/1":
			fmt.Fprint(w, `{"id":"1","name":"Srv","environment":{"type":"fabric"}}`)
		case r.URL.Path == "/api/servers/1/files/list" && r.URL.Query().Get("path") == "mods":
			fmt.Fprint(w, `[{"name":"sodium-1.0.jar","is_dir":false}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	initSecrets(t, db)
	if err := pppkg.Set(pppkg.Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("set creds: %v", err)
	}
	inst := dbpkg.Instance{Name: "", Loader: "", EnforceSameLoader: true}
	if err := dbpkg.InsertInstance(db, &inst); err != nil {
		t.Fatalf("insert inst: %v", err)
	}
	oldClient := modClient
	modClient = matchClient{}
	defer func() { modClient = oldClient }()

	h := syncHandler(db)
	body := fmt.Sprintf(`{"serverId":"1","instanceId":%d}`, inst.ID)
	req := httptest.NewRequest(http.MethodPost, "/api/pufferpanel/sync", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var resp struct {
		Instance  dbpkg.Instance `json:"instance"`
		Unmatched []string       `json:"unmatched"`
		Mods      []dbpkg.Mod    `json:"mods"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Unmatched) != 0 {
		t.Fatalf("unexpected unmatched %v", resp.Unmatched)
	}
	if len(resp.Mods) != 1 || resp.Mods[0].CurrentVersion != "1.0" {
		t.Fatalf("unexpected mods %+v", resp.Mods)
	}
}

func TestSyncHandler_DeepScanMatches(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	fw, _ := zw.Create("fabric.mod.json")
	fmt.Fprint(fw, `{"id":"sodium","version":"1.0"}`)
	zw.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case r.URL.Path == "/api/servers/1":
			fmt.Fprint(w, `{"id":"1","name":"Srv","environment":{"type":"fabric"}}`)
		case r.URL.Path == "/api/servers/1/files/list" && r.URL.Query().Get("path") == "mods":
			fmt.Fprint(w, `[{"name":"m.jar","is_dir":false}]`)
		case r.URL.Path == "/api/servers/1/files/contents" && r.URL.Query().Get("path") == "mods/m.jar":
			w.Write(buf.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	initSecrets(t, db)
	if err := pppkg.Set(pppkg.Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret", DeepScan: true}); err != nil {
		t.Fatalf("set creds: %v", err)
	}
	inst := dbpkg.Instance{Name: "", Loader: "", EnforceSameLoader: true}
	if err := dbpkg.InsertInstance(db, &inst); err != nil {
		t.Fatalf("insert inst: %v", err)
	}
	oldClient := modClient
	modClient = matchClient{}
	defer func() { modClient = oldClient }()

	h := syncHandler(db)
	body := fmt.Sprintf(`{"serverId":"1","instanceId":%d}`, inst.ID)
	req := httptest.NewRequest(http.MethodPost, "/api/pufferpanel/sync", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var resp struct {
		Instance  dbpkg.Instance `json:"instance"`
		Unmatched []string       `json:"unmatched"`
		Mods      []dbpkg.Mod    `json:"mods"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Unmatched) != 0 {
		t.Fatalf("unexpected unmatched %v", resp.Unmatched)
	}
	if len(resp.Mods) != 1 || resp.Mods[0].CurrentVersion != "1.0" {
		t.Fatalf("unexpected mods %+v", resp.Mods)
	}
}

func TestSyncHandler_Validation(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	h := syncHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/pufferpanel/sync", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d", w.Code)
	}
	var resp httpx.Error
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Details["serverId"] == "" || resp.Details["instanceId"] == "" {
		t.Fatalf("details %v", resp.Details)
	}
}

func TestResyncInstanceHandler_Idempotent(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	// prepare instance and mod
	inst := dbpkg.Instance{Name: "Srv", Loader: "fabric", EnforceSameLoader: true, PufferpanelServerID: "1"}
	if err := dbpkg.InsertInstance(db, &inst); err != nil {
		t.Fatalf("insert inst: %v", err)
	}
	mod := dbpkg.Mod{Name: "Sodium", URL: "https://modrinth.com/mod/sodium", GameVersion: "1.20", Loader: "fabric", Channel: "release", CurrentVersion: "1.0", AvailableVersion: "1.0", AvailableChannel: "release", DownloadURL: "http://example.com", InstanceID: inst.ID}
	if err := dbpkg.InsertMod(db, &mod); err != nil {
		t.Fatalf("insert mod: %v", err)
	}

	// mock server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case r.URL.Path == "/api/servers/1/files/list" && r.URL.Query().Get("path") == "mods":
			fmt.Fprint(w, `[{"name":"sodium-2.0.jar","is_dir":false}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	initSecrets(t, db)
	if err := pppkg.Set(pppkg.Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("set creds: %v", err)
	}

	// custom mod client returning version 2.0
	oldClient := modClient
	modClient = resyncClient{}
	defer func() { modClient = oldClient }()

	h := resyncInstanceHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/instances/"+strconv.Itoa(inst.ID)+"/resync", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(inst.ID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	w2 := httptest.NewRecorder()
	h(w2, req)
	if w2.Code != http.StatusOK {
		t.Fatalf("status2 %d", w2.Code)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM updates`).Scan(&count); err != nil {
		t.Fatalf("count updates: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 update, got %d", count)
	}
}

type resyncClient struct{}

func (resyncClient) Project(ctx context.Context, slug string) (*mr.Project, error) {
	return &mr.Project{Title: "Sodium", IconURL: ""}, nil
}

func (resyncClient) Versions(ctx context.Context, slug, gameVersion, loader string) ([]mr.Version, error) {
	return []mr.Version{{
		ID:            "1",
		VersionNumber: "2.0",
		VersionType:   "release",
		GameVersions:  []string{"1.20"},
		Loaders:       []string{"fabric"},
		Files:         []mr.VersionFile{{URL: "http://example.com"}},
	}}, nil
}

func (resyncClient) Search(ctx context.Context, query string) (*mr.SearchResult, error) {
	return &mr.SearchResult{Hits: []struct {
		ProjectID string `json:"project_id"`
		Slug      string `json:"slug"`
		Title     string `json:"title"`
	}{{ProjectID: "1", Slug: "sodium", Title: "Sodium"}}}, nil
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

func initSecrets(t *testing.T, db *sql.DB) {
	t.Helper()
	t.Setenv("SECRET_KEYSET", `{"primary":"1","keys":{"1":"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"}}`)
	km, err := secrets.Load(context.Background())
	if err != nil {
		t.Fatalf("load keys: %v", err)
	}
	svc := secrets.NewService(db, km)
	tokenpkg.Init(svc)
	pppkg.Init(svc)
}

func TestSecretSettings_Flow(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	t.Setenv("SECRET_KEYSET", `{"primary":"1","keys":{"1":"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"}}`)
	km, err := secrets.Load(context.Background())
	if err != nil {
		t.Fatalf("load keys: %v", err)
	}
	svc := secrets.NewService(db, km)
	tokenpkg.Init(svc)
	pppkg.Init(svc)

	setH := setSecretHandler()
	statusH := secretStatusHandler(svc)
	delH := deleteSecretHandler()

	// set secret
	reqSet := httptest.NewRequest(http.MethodPost, "/api/settings/secret/modrinth", strings.NewReader(`{"token":"abcd1234"}`))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "modrinth")
	reqSet = reqSet.WithContext(context.WithValue(reqSet.Context(), chi.RouteCtxKey, rctx))
	wSet := httptest.NewRecorder()
	setH(wSet, reqSet)
	if wSet.Code != http.StatusNoContent {
		t.Fatalf("set status %d", wSet.Code)
	}

	// status should show last4
	reqStat := httptest.NewRequest(http.MethodGet, "/api/settings/secret/modrinth/status", nil)
	rctx2 := chi.NewRouteContext()
	rctx2.URLParams.Add("type", "modrinth")
	reqStat = reqStat.WithContext(context.WithValue(reqStat.Context(), chi.RouteCtxKey, rctx2))
	wStat := httptest.NewRecorder()
	statusH(wStat, reqStat)
	if wStat.Code != http.StatusOK {
		t.Fatalf("status code %d", wStat.Code)
	}
	var st struct {
		Exists    bool      `json:"exists"`
		Last4     string    `json:"last4"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	if err := json.NewDecoder(wStat.Body).Decode(&st); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !st.Exists || st.Last4 != "1234" || st.UpdatedAt.IsZero() {
		t.Fatalf("unexpected status: %+v", st)
	}

	// delete secret
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/settings/secret/modrinth", nil)
	rctx3 := chi.NewRouteContext()
	rctx3.URLParams.Add("type", "modrinth")
	reqDel = reqDel.WithContext(context.WithValue(reqDel.Context(), chi.RouteCtxKey, rctx3))
	wDel := httptest.NewRecorder()
	delH(wDel, reqDel)
	if wDel.Code != http.StatusNoContent {
		t.Fatalf("delete status %d", wDel.Code)
	}

	// status again should show non-existence
	reqStat2 := httptest.NewRequest(http.MethodGet, "/api/settings/secret/modrinth/status", nil)
	rctx4 := chi.NewRouteContext()
	rctx4.URLParams.Add("type", "modrinth")
	reqStat2 = reqStat2.WithContext(context.WithValue(reqStat2.Context(), chi.RouteCtxKey, rctx4))
	wStat2 := httptest.NewRecorder()
	statusH(wStat2, reqStat2)
	if wStat2.Code != http.StatusOK {
		t.Fatalf("status2 code %d", wStat2.Code)
	}
	var st2 struct {
		Exists bool `json:"exists"`
	}
	if err := json.NewDecoder(wStat2.Body).Decode(&st2); err != nil {
		t.Fatalf("decode status2: %v", err)
	}
	if st2.Exists {
		t.Fatalf("expected secret deleted")
	}
}

func TestSecurityMiddleware(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	t.Setenv("SECRET_KEYSET", `{"primary":"1","keys":{"1":"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"}}`)
	t.Setenv("ADMIN_TOKEN", "admintok")
	var dist embed.FS
	h := New(db, dist)

	// unauthorized
	req0 := httptest.NewRequest(http.MethodGet, "/api/settings/secret/modrinth/status", nil)
	w0 := httptest.NewRecorder()
	h.ServeHTTP(w0, req0)
	if w0.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden, got %d", w0.Code)
	}

	// fetch token
	req1 := httptest.NewRequest(http.MethodGet, "/api/settings/secret/modrinth/status", nil)
	req1.Header.Set("Authorization", "Bearer admintok")
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("status %d", w1.Code)
	}
	if w1.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("missing csp header")
	}
	if cc := w1.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("unexpected cache-control %q", cc)
	}
	var csrf string
	for _, c := range w1.Result().Cookies() {
		if c.Name == "csrf_token" {
			csrf = c.Value
		}
	}
	if csrf == "" {
		t.Fatalf("missing csrf cookie")
	}

	// missing csrf header
	req2 := httptest.NewRequest(http.MethodPost, "/api/settings/secret/modrinth", strings.NewReader(`{"token":"abcd1234"}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer admintok")
	req2.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("expected csrf forbidden, got %d", w2.Code)
	}

	// valid csrf
	req3 := httptest.NewRequest(http.MethodPost, "/api/settings/secret/modrinth", strings.NewReader(`{"token":"abcd1234"}`))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("Authorization", "Bearer admintok")
	req3.Header.Set("X-CSRF-Token", csrf)
	req3.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, req3)
	if w3.Code != http.StatusNoContent {
		t.Fatalf("set status %d", w3.Code)
	}
	if cc := w3.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("unexpected cache-control %q", cc)
	}
}

func TestSecurityMiddleware_NoAdminToken(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	t.Setenv("SECRET_KEYSET", `{"primary":"1","keys":{"1":"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"}}`)
	dist := testFS
	h := New(db, dist)

	req1 := httptest.NewRequest(http.MethodGet, "/api/settings/secret/modrinth/status", nil)
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("status %d", w1.Code)
	}
	var csrf string
	for _, c := range w1.Result().Cookies() {
		if c.Name == "csrf_token" {
			csrf = c.Value
		}
	}
	if csrf == "" {
		t.Fatalf("missing csrf cookie")
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/settings/secret/modrinth", strings.NewReader(`{"token":"abcd1234"}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-CSRF-Token", csrf)
	req2.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("set status %d", w2.Code)
	}
}

func TestSecurityHeaders_CSP(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	t.Setenv("SECRET_KEYSET", `{"primary":"1","keys":{"1":"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"}}`)
	dist, err := fs.Sub(testFS, "testdata")
	if err != nil {
		t.Fatalf("sub fs: %v", err)
	}

	t.Setenv("APP_ENV", "development")
	h := New(db, dist)
	if err := pppkg.Set(pppkg.Credentials{BaseURL: "https://pp.example.com", ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("set creds: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/instances", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "style-src 'self' 'unsafe-inline'") {
		t.Fatalf("dev csp missing unsafe-inline: %s", csp)
	}
	if !strings.Contains(csp, "connect-src 'self' https://pp.example.com") {
		t.Fatalf("dev csp missing connect-src: %s", csp)
	}
	if !strings.Contains(csp, "img-src 'self' data: https:") {
		t.Fatalf("dev csp missing img-src: %s", csp)
	}

	t.Setenv("APP_ENV", "production")
	h = New(db, dist)
	if err := pppkg.Set(pppkg.Credentials{BaseURL: "https://pp.example.com", ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("set creds prod: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/instances", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	csp = w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "style-src 'self' 'nonce-") {
		t.Fatalf("prod csp missing nonce: %s", csp)
	}
	if strings.Contains(csp, "unsafe-inline") {
		t.Fatalf("prod csp should not allow unsafe-inline: %s", csp)
	}
	if !strings.Contains(csp, "connect-src 'self' https://pp.example.com") {
		t.Fatalf("prod csp missing connect-src: %s", csp)
	}
	if !strings.Contains(csp, "img-src 'self' data: https:") {
		t.Fatalf("prod csp missing img-src: %s", csp)
	}

	// index should include the style nonce meta tag
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	body := w.Body.String()
	prodCSP := w.Header().Get("Content-Security-Policy")
	re := regexp.MustCompile(`nonce-([^']+)`)
	m := re.FindStringSubmatch(prodCSP)
	if len(m) < 2 {
		t.Fatalf("could not extract nonce from csp: %s", prodCSP)
	}
	if !strings.Contains(body, "<meta name=\"csp-nonce\" content=\""+m[1]+"\">") {
		t.Fatalf("missing nonce meta: %s", body)
	}
}

func TestCheckModHandler_NoWrite(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	inst := &dbpkg.Instance{Name: "A", Loader: "fabric"}
	if err := dbpkg.InsertInstance(db, inst); err != nil {
		t.Fatalf("insert inst: %v", err)
	}
	mod := &dbpkg.Mod{
		Name:             "M1",
		URL:              "https://modrinth.com/mod/fake",
		GameVersion:      "1.20",
		Loader:           "fabric",
		Channel:          "release",
		CurrentVersion:   "0.9",
		AvailableVersion: "0.8",
		InstanceID:       inst.ID,
	}
	if err := dbpkg.InsertMod(db, mod); err != nil {
		t.Fatalf("insert mod: %v", err)
	}

	oldClient := modClient
	modClient = fakeModClient{}
	defer func() { modClient = oldClient }()

	h := checkModHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/mods/"+strconv.Itoa(mod.ID)+"/check", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(mod.ID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var resp dbpkg.Mod
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AvailableVersion != "1.0" {
		t.Fatalf("want available 1.0, got %q", resp.AvailableVersion)
	}
	m2, err := dbpkg.GetMod(db, mod.ID)
	if err != nil {
		t.Fatalf("get mod: %v", err)
	}
	if m2.AvailableVersion != "0.8" {
		t.Fatalf("db modified to %q", m2.AvailableVersion)
	}
}

func TestCheckModHandler_MissingToken(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	inst := &dbpkg.Instance{Name: "A", Loader: "fabric"}
	if err := dbpkg.InsertInstance(db, inst); err != nil {
		t.Fatalf("insert inst: %v", err)
	}
	mod := &dbpkg.Mod{
		Name:        "M1",
		URL:         "https://modrinth.com/mod/fake",
		GameVersion: "1.20",
		Loader:      "fabric",
		Channel:     "release",
		InstanceID:  inst.ID,
	}
	if err := dbpkg.InsertMod(db, mod); err != nil {
		t.Fatalf("insert mod: %v", err)
	}

	oldClient := modClient
	modClient = errClient{}
	defer func() { modClient = oldClient }()

	h := checkModHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/mods/"+strconv.Itoa(mod.ID)+"/check", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(mod.ID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", w.Code)
	}
}

func TestMetadataHandler_Proxy(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	initSecrets(t, db)
	if err := tokenpkg.SetToken("tok"); err != nil {
		t.Fatalf("set token: %v", err)
	}
	oldClient := modClient
	modClient = fakeModClient{}
	defer func() { modClient = oldClient }()

	h := metadataHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/mods/metadata", strings.NewReader(`{"url":"https://modrinth.com/mod/fake"}`))
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var resp struct {
		Versions []mr.Version `json:"versions"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Versions) != 1 || resp.Versions[0].ID != "1" {
		t.Fatalf("unexpected resp %+v", resp)
	}
}

func TestMetadataHandler_MissingToken(t *testing.T) {
	oldClient := modClient
	modClient = errClient{}
	defer func() { modClient = oldClient }()
	h := metadataHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/mods/metadata", strings.NewReader(`{"url":"https://modrinth.com/mod/fake"}`))
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", w.Code)
	}
}

func TestTestPufferHandler_ReturnsJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	h := testPufferHandler()
	body := fmt.Sprintf(`{"base_url":%q,"client_id":"id","client_secret":"secret"}`, srv.URL)
	req := httptest.NewRequest(http.MethodPost, "/api/pufferpanel/test", strings.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type %s", ct)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["message"] != "ok" {
		t.Fatalf("unexpected resp %+v", resp)
	}
}

func TestListServersHandler_OK(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	initSecrets(t, db)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case "/api/servers":
			fmt.Fprint(w, `{"paging":{"page":0,"size":2,"total":2},"servers":[{"id":"1","name":"One"},{"id":"2","name":"Two"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	if err := pppkg.Set(pppkg.Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("set creds: %v", err)
	}
	h := listServersHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/pufferpanel/servers", nil)
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var servers []pppkg.Server
	if err := json.NewDecoder(w.Body).Decode(&servers); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(servers) != 2 || servers[0].ID != "1" || servers[1].ID != "2" {
		t.Fatalf("unexpected servers %+v", servers)
	}
}

func TestListServersHandler_PropagatesError(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	initSecrets(t, db)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case "/api/servers":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"code":403,"message":"nope","requestId":"x"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	if err := pppkg.Set(pppkg.Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("set creds: %v", err)
	}
	h := listServersHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/pufferpanel/servers", nil)
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status %d", w.Code)
	}
	var e httpx.Error
	if err := json.NewDecoder(w.Body).Decode(&e); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Code != "forbidden" || e.Message != "nope" {
		t.Fatalf("unexpected error %+v", e)
	}
	if e.RequestID == "" {
		t.Fatal("missing requestId")
	}
}

func TestListServersHandler_Upstream400(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	initSecrets(t, db)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case "/api/servers":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"code":400,"message":"bad","requestId":"x"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	if err := pppkg.Set(pppkg.Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("set creds: %v", err)
	}
	h := listServersHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/pufferpanel/servers", nil)
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d", w.Code)
	}
	var e httpx.Error
	if err := json.NewDecoder(w.Body).Decode(&e); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Code != "bad_request" || e.Message != "bad" {
		t.Fatalf("unexpected error %+v", e)
	}
	if e.RequestID == "" {
		t.Fatal("missing requestId")
	}
}

func TestListServersHandler_Upstream401(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	initSecrets(t, db)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case "/api/servers":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"code":401,"message":"unauth","requestId":"x"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	if err := pppkg.Set(pppkg.Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("set creds: %v", err)
	}
	h := listServersHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/pufferpanel/servers", nil)
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", w.Code)
	}
	var e httpx.Error
	if err := json.NewDecoder(w.Body).Decode(&e); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Code != "token_required" || e.Message != "unauth" {
		t.Fatalf("unexpected error %+v", e)
	}
	if e.RequestID == "" {
		t.Fatal("missing requestId")
	}
}

func TestListServersHandler_Upstream5xx(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	initSecrets(t, db)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case "/api/servers":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"code":500,"message":"broken","requestId":"x"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	if err := pppkg.Set(pppkg.Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("set creds: %v", err)
	}
	h := listServersHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/pufferpanel/servers", nil)
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type %s", ct)
	}
	var e httpx.Error
	if err := json.NewDecoder(w.Body).Decode(&e); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Code != "bad_gateway" || e.Message != "broken" {
		t.Fatalf("unexpected error %+v", e)
	}
	if e.RequestID == "" {
		t.Fatal("missing requestId")
	}
}

func TestTestPufferHandler_InvalidBaseURL(t *testing.T) {
	h := testPufferHandler()
	body := `{"base_url":"ftp://example.com","client_id":"id","client_secret":"secret"}`
	req := httptest.NewRequest(http.MethodPost, "/api/pufferpanel/test", strings.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d", w.Code)
	}
	var e httpx.Error
	if err := json.NewDecoder(w.Body).Decode(&e); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Message != "invalid base_url scheme" {
		t.Fatalf("unexpected error %+v", e)
	}
}

func TestListServersHandler_BadConfig(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	initSecrets(t, db)
	h := listServersHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/pufferpanel/servers", nil)
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d", w.Code)
	}
	var e httpx.Error
	if err := json.NewDecoder(w.Body).Decode(&e); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Message != "base_url required" {
		t.Fatalf("unexpected error %+v", e)
	}
}

func TestSetCredentials_TrimsSlash(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	initSecrets(t, db)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	raw := srv.URL + "/"
	if err := pppkg.Set(pppkg.Credentials{BaseURL: raw, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("set creds: %v", err)
	}
	c, err := pppkg.Config()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if c.BaseURL != srv.URL {
		t.Fatalf("base url %s", c.BaseURL)
	}
}

func TestSecretStatus_PufferpanelMissing(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	t.Setenv("SECRET_KEYSET", `{"primary":"1","keys":{"1":"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"}}`)
	km, err := secrets.Load(context.Background())
	if err != nil {
		t.Fatalf("load keys: %v", err)
	}
	svc := secrets.NewService(db, km)
	h := secretStatusHandler(svc)
	req := httptest.NewRequest(http.MethodGet, "/api/settings/secret/pufferpanel/status", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "pufferpanel")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var st struct {
		Exists bool `json:"exists"`
	}
	if err := json.NewDecoder(w.Body).Decode(&st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st.Exists {
		t.Fatalf("expected not exists")
	}
}

func TestSecretStatus_PufferpanelInvalid(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	t.Setenv("SECRET_KEYSET", `{"primary":"1","keys":{"1":"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"}}`)
	km1, err := secrets.Load(context.Background())
	if err != nil {
		t.Fatalf("load keys1: %v", err)
	}
	svc1 := secrets.NewService(db, km1)
	pppkg.Init(svc1)
	if err := pppkg.Set(pppkg.Credentials{BaseURL: "http://panel", ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("set creds: %v", err)
	}
	t.Setenv("SECRET_KEYSET", `{"primary":"2","keys":{"2":"202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f"}}`)
	km2, err := secrets.Load(context.Background())
	if err != nil {
		t.Fatalf("load keys2: %v", err)
	}
	svc2 := secrets.NewService(db, km2)
	h := secretStatusHandler(svc2)
	req := httptest.NewRequest(http.MethodGet, "/api/settings/secret/pufferpanel/status", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "pufferpanel")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status %d", w.Code)
	}
	var e httpx.Error
	if err := json.NewDecoder(w.Body).Decode(&e); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Code != "not_found" {
		t.Fatalf("code %s", e.Code)
	}
}
