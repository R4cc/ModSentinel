package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"embed"
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
	pppkg "modsentinel/internal/pufferpanel"
	"modsentinel/internal/secrets"
	tokenpkg "modsentinel/internal/token"

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

	h := syncHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/pufferpanel/sync?server=1", nil)
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

	oldClient := modClient
	modClient = matchClient{}
	defer func() { modClient = oldClient }()

	h := syncHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/pufferpanel/sync?server=1", nil)
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

	oldClient := modClient
	modClient = matchClient{}
	defer func() { modClient = oldClient }()

	h := syncHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/pufferpanel/sync?server=1", nil)
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
