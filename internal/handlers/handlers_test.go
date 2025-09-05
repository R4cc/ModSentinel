package handlers

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"sync"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	dbpkg "modsentinel/internal/db"
	"modsentinel/internal/httpx"
	logx "modsentinel/internal/logx"
	mr "modsentinel/internal/modrinth"
	oauth "modsentinel/internal/oauth"
	pppkg "modsentinel/internal/pufferpanel"
	"modsentinel/internal/secrets"
	settingspkg "modsentinel/internal/settings"
	tokenpkg "modsentinel/internal/token"

	_ "modernc.org/sqlite"
)

//go:embed testdata/**
var testFS embed.FS

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbpkg.Init(db); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := dbpkg.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	stop := StartJobQueue(context.Background(), db)
	t.Cleanup(func() { stop(context.Background()) })
	return db
}

func waitJob(t *testing.T, db *sql.DB, id int) dbpkg.SyncJob {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		job, err := dbpkg.GetSyncJob(db, id)
		if err != nil {
			t.Fatalf("get job: %v", err)
		}
		switch job.Status {
		case JobSucceeded, JobFailed, JobCanceled:
			return *job
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for job, status %s", job.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
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
        ProjectID   string `json:"project_id"`
        Slug        string `json:"slug"`
        Title       string `json:"title"`
        Description string `json:"description"`
        IconURL     string `json:"icon_url"`
        Downloads   int    `json:"downloads"`
    }{{ProjectID: "1", Slug: query, Title: "Fake", Description: "", IconURL: "", Downloads: 0}}}, nil
}

func (fakeModClient) Resolve(ctx context.Context, slug string) (*mr.Project, string, error) {
	return &mr.Project{Title: "Fake", IconURL: ""}, slug, nil
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

func (matchClient) Resolve(ctx context.Context, slug string) (*mr.Project, string, error) {
	return &mr.Project{Title: "Sodium", IconURL: ""}, "sodium", nil
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

func (errClient) Resolve(ctx context.Context, slug string) (*mr.Project, string, error) {
	return nil, "", &mr.Error{Status: http.StatusUnauthorized}
}

func (matchClient) Search(ctx context.Context, query string) (*mr.SearchResult, error) {
    return &mr.SearchResult{Hits: []struct {
        ProjectID   string `json:"project_id"`
        Slug        string `json:"slug"`
        Title       string `json:"title"`
        Description string `json:"description"`
        IconURL     string `json:"icon_url"`
        Downloads   int    `json:"downloads"`
    }{{ProjectID: "1", Slug: "sodium", Title: "Sodium", Description: "", IconURL: "", Downloads: 0}}}, nil
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

	// stub PufferPanel interactions
	origGet := ppGetServer
	origList := ppListPath
	defer func() { ppGetServer = origGet; ppListPath = origList }()
	ppGetServer = func(ctx context.Context, id string) (*pppkg.ServerDetail, error) {
		return &pppkg.ServerDetail{ID: id, Name: "Srv", Environment: struct {
			Type string `json:"type"`
		}{Type: "fabric"}}, nil
	}
	ppListPath = func(ctx context.Context, serverID, path string) ([]pppkg.FileEntry, error) { return nil, nil }

	create := createInstanceHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/instances", strings.NewReader(`{"name":"A","loader":"fabric","serverId":"1"}`))
	w := httptest.NewRecorder()
	create(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status %d", w.Code)
	}
	var inst dbpkg.Instance
	if err := json.NewDecoder(w.Body).Decode(&inst); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	// Update instance name
	update := updateInstanceHandler(db)
	payload := `{"name":"A2"}`
	req3 := httptest.NewRequest(http.MethodPut, "/api/instances/"+strconv.Itoa(inst.ID), strings.NewReader(payload))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(inst.ID))
	req3 = req3.WithContext(context.WithValue(req3.Context(), chi.RouteCtxKey, rctx))
	w3 := httptest.NewRecorder()
	update(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("update status %d", w3.Code)
	}

	// Delete instance
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
}

func TestValidateAndCreateInstance(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	// stub pufferpanel functions
	origGet := ppGetServer
	origList := ppListPath
	defer func() {
		ppGetServer = origGet
		ppListPath = origList
	}()
	ppGetServer = func(ctx context.Context, id string) (*pppkg.ServerDetail, error) {
		return &pppkg.ServerDetail{ID: id, Name: "Srv", Environment: struct {
			Type string `json:"type"`
		}{Type: "fabric"}}, nil
	}
	ppListPath = func(ctx context.Context, serverID, path string) ([]pppkg.FileEntry, error) {
		return nil, nil
	}

	validate := validateInstanceHandler()
	reqV := httptest.NewRequest(http.MethodPost, "/api/instances/validate", strings.NewReader(`{"name":"A","loader":"fabric","serverId":"1"}`))
	wV := httptest.NewRecorder()
	validate(wV, reqV)
	if wV.Code != http.StatusOK {
		t.Fatalf("validate status %d", wV.Code)
	}

	create := createInstanceHandler(db)
	reqC := httptest.NewRequest(http.MethodPost, "/api/instances", strings.NewReader(`{"name":"A","loader":"fabric","serverId":"1"}`))
	wC := httptest.NewRecorder()
	create(wC, reqC)
	if wC.Code != http.StatusCreated {
		t.Fatalf("create status %d", wC.Code)
	}
	var inst dbpkg.Instance
	if err := json.NewDecoder(wC.Body).Decode(&inst); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if inst.Name != "A" || inst.ID == 0 {
		t.Fatalf("unexpected instance %+v", inst)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM instances`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 rows, got %d", count)
	}
}

func TestCreateInstance_InvalidServer(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	origGet := ppGetServer
	defer func() { ppGetServer = origGet }()
	ppGetServer = func(ctx context.Context, id string) (*pppkg.ServerDetail, error) {
		return nil, pppkg.ErrNotFound
	}

	create := createInstanceHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/instances", strings.NewReader(`{"name":"A","loader":"fabric","serverId":"missing"}`))
	w := httptest.NewRecorder()
	create(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM instances`).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 row, got %d", count)
	}
}

func TestInstanceNameValidation(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	origGet := ppGetServer
	origList := ppListPath
	defer func() { ppGetServer = origGet; ppListPath = origList }()
	ppGetServer = func(ctx context.Context, id string) (*pppkg.ServerDetail, error) {
		return &pppkg.ServerDetail{ID: id, Name: "Srv", Environment: struct {
			Type string `json:"type"`
		}{Type: "fabric"}}, nil
	}
	ppListPath = func(ctx context.Context, serverID, path string) ([]pppkg.FileEntry, error) { return nil, nil }

	create := createInstanceHandler(db)
	for _, n := range []string{"", "   "} {
		req := httptest.NewRequest(http.MethodPost, "/api/instances", strings.NewReader(fmt.Sprintf(`{"name":%q,"loader":"fabric","serverId":"1"}`, n)))
		w := httptest.NewRecorder()
		create(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for %q, got %d", n, w.Code)
		}
	}
	longName := strings.Repeat("a", dbpkg.InstanceNameMaxLen+1)
	req := httptest.NewRequest(http.MethodPost, "/api/instances", strings.NewReader(fmt.Sprintf(`{"name":%q,"loader":"fabric","serverId":"1"}`, longName)))
	w := httptest.NewRecorder()
	create(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for long name, got %d", w.Code)
	}

	inst := dbpkg.Instance{Name: "ok", Loader: "fabric", EnforceSameLoader: true}
	if err := dbpkg.InsertInstance(db, &inst); err != nil {
		t.Fatalf("insert inst: %v", err)
	}
	update := updateInstanceHandler(db)
	reqU := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/instances/%d", inst.ID), strings.NewReader(`{"name":" "}`))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(inst.ID))
	reqU = reqU.WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rctx))
	wU := httptest.NewRecorder()
	update(wU, reqU)
	if wU.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on update, got %d", wU.Code)
	}
}

func TestSyncHandler_ScansMods(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case r.URL.Path == "/api/servers/1":
			fmt.Fprint(w, `{"id":"1","name":"Srv","environment":{"type":"fabric"}}`)
		case r.URL.Path == "/api/servers/1/file/mods%2F":
			fmt.Fprint(w, `[{"name":"mod.jar","is_dir":false},{"name":"other.txt","is_dir":false}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	_, _, _ = initSecrets(t, db)
	if err := pppkg.Set(pppkg.Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("set creds: %v", err)
	}
	inst := dbpkg.Instance{Name: "Inst", Loader: "", EnforceSameLoader: true}
	if err := dbpkg.InsertInstance(db, &inst); err != nil {
		t.Fatalf("insert inst: %v", err)
	}
	h := syncHandler(db)
	body := `{"serverId":"1"}`
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/instances/%d/sync", inst.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(inst.ID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var resp struct {
		ID     int    `json:"id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != JobQueued {
		t.Fatalf("unexpected status %s", resp.Status)
	}
	job := waitJob(t, db, resp.ID)
	if job.Status != JobSucceeded {
		t.Fatalf("final status %s", job.Status)
	}
	mods, err := dbpkg.ListMods(db, inst.ID)
	if err != nil {
		t.Fatalf("list mods: %v", err)
	}
	if len(mods) != 0 {
		t.Fatalf("expected no mods, got %d", len(mods))
	}
}

func TestSyncHandler_MissingFolder(t *testing.T) {
	t.Skip("TODO: update for job queue")
}

func TestSyncHandler_MatchesMods(t *testing.T) {
	t.Skip("TODO: update for job queue")
}

func TestSyncHandler_DeepScanMatches(t *testing.T) {
	t.Skip("TODO: update for job queue")
}

func TestSyncHandler_Validation(t *testing.T) {
	t.Skip("TODO: update for job queue")
}

func TestSyncHandler_UsesStoredServerID(t *testing.T) {
	t.Skip("TODO: update for job queue")
}

func TestPufferpanelTestEndpointPostOnly(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	svc, _, _ := initSecrets(t, db)
	h := New(db, os.DirFS("."), svc)

	req := httptest.NewRequest(http.MethodGet, "/api/pufferpanel/test", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status %d", w.Code)
	}
}

func TestSyncRoutesPostOnly(t *testing.T) {
	prev := allowResyncAlias
	allowResyncAlias = true
	t.Cleanup(func() { allowResyncAlias = prev })
	db := openTestDB(t)
	defer db.Close()
	svc, _, _ := initSecrets(t, db)
	h := New(db, os.DirFS("."), svc)

	req := httptest.NewRequest(http.MethodGet, "/api/instances/1/resync", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/instances/1/sync", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", w.Code)
	}
}

func TestSyncHandler_ResyncAlias(t *testing.T) {
	t.Skip("TODO: update for job queue")
}

func TestSyncHandler_RequestCanceled(t *testing.T) {
	t.Skip("TODO: update for job queue")
}

func TestResyncAliasDisabled(t *testing.T) {
	prev := allowResyncAlias
	allowResyncAlias = false
	t.Cleanup(func() { allowResyncAlias = prev })
	resyncAliasHits.Store(0)
	db := openTestDB(t)
	defer db.Close()
	svc, _, _ := initSecrets(t, db)
	h := New(db, os.DirFS("."), svc)

	req := httptest.NewRequest(http.MethodPost, "/api/instances/1/resync", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusGone {
		t.Fatalf("status %d", w.Code)
	}
	if n := resyncAliasHits.Load(); n != 0 {
		t.Fatalf("alias hits %d", n)
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

func initSecrets(t *testing.T, db *sql.DB) (*secrets.Service, *settingspkg.Store, *oauth.Service) {
	t.Helper()
	svc := secrets.NewService(db)
	cfg := settingspkg.New(db)
	oauthSvc := oauth.New(db)
	tokenpkg.Init(svc)
	pppkg.Init(svc, cfg, oauthSvc)
	return svc, cfg, oauthSvc
}

func TestSecretSettings_Flow(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	svc := secrets.NewService(db)
	cfg := settingspkg.New(db)
	oauthSvc := oauth.New(db)
	tokenpkg.Init(svc)
	pppkg.Init(svc, cfg, oauthSvc)

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
	t.Setenv("ADMIN_TOKEN", "admintok")
	var dist embed.FS
	svc, _, _ := initSecrets(t, db)
	h := New(db, dist, svc)

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
	dist := testFS
	svc, _, _ := initSecrets(t, db)
	h := New(db, dist, svc)

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
	dist, err := fs.Sub(testFS, "testdata")
	if err != nil {
		t.Fatalf("sub fs: %v", err)
	}

	t.Setenv("APP_ENV", "development")
	svc, _, _ := initSecrets(t, db)
	h := New(db, dist, svc)
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
	h = New(db, dist, svc)
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

	_, _, _ = initSecrets(t, db)
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

func TestInstancesSyncHandler_OK(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	_, _, _ = initSecrets(t, db)
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
	// pre-insert instance to verify name preservation
	inst := dbpkg.Instance{Name: "Old", Loader: "fabric", EnforceSameLoader: true, PufferpanelServerID: "1"}
	if err := dbpkg.InsertInstance(db, &inst); err != nil {
		t.Fatalf("insert inst: %v", err)
	}
	h := listServersHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/instances/sync", nil)
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
	inst2, err := dbpkg.GetInstance(db, inst.ID)
	if err != nil {
		t.Fatalf("get inst: %v", err)
	}
	if inst2.Name != "Old" {
		t.Fatalf("instance name %s", inst2.Name)
	}
	var name2 string
	if err := db.QueryRow(`SELECT name FROM instances WHERE pufferpanel_server_id=?`, "2").Scan(&name2); err != nil {
		t.Fatalf("get inst2: %v", err)
	}
	if name2 != "Two" {
		t.Fatalf("instance2 name %s", name2)
	}
}

func TestInstancesSyncHandler_Truncate(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	_, _, _ = initSecrets(t, db)
	longName := strings.Repeat("a", dbpkg.InstanceNameMaxLen+10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case "/api/servers":
			fmt.Fprintf(w, `{"paging":{"page":0,"size":1,"total":1},"servers":[{"id":"1","name":"%s"}]}`, longName)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	if err := pppkg.Set(pppkg.Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("set creds: %v", err)
	}
	h := listServersHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/instances/sync", nil)
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM instances WHERE pufferpanel_server_id=?`, "1").Scan(&name); err != nil {
		t.Fatalf("get inst: %v", err)
	}
	if l := len([]rune(name)); l != dbpkg.InstanceNameMaxLen {
		t.Fatalf("name len %d", l)
	}
	if name == "" {
		t.Fatalf("name empty")
	}
}

func TestInstancesSyncHandler_Auth(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	_, _, _ = initSecrets(t, db)
	t.Setenv("ADMIN_TOKEN", "admintok")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case "/api/servers":
			fmt.Fprint(w, `{"paging":{"page":0,"size":1,"total":1},"servers":[{"id":"1","name":"One"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	if err := pppkg.Set(pppkg.Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("set creds: %v", err)
	}
	h := requireAuth()(listServersHandler(db))

	req1 := httptest.NewRequest(http.MethodPost, "/api/instances/sync", nil)
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req1)
	if w1.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status %d", w1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/instances/sync", nil)
	req2.Header.Set("Authorization", "Bearer admintok")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("status %d", w2.Code)
	}
	var servers []pppkg.Server
	if err := json.NewDecoder(w2.Body).Decode(&servers); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(servers) != 1 || servers[0].ID != "1" {
		t.Fatalf("unexpected servers %+v", servers)
	}
}

func TestInstancesSyncHandler_DedupeAndCache(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	_, _, _ = initSecrets(t, db)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case "/api/servers":
			atomic.AddInt32(&calls, 1)
			fmt.Fprint(w, `{"paging":{"page":0,"size":1,"total":1},"servers":[{"id":"1","name":"One"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	if err := pppkg.Set(pppkg.Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("set creds: %v", err)
	}
	h := listServersHandler(db)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/instances/sync", nil)
			w := httptest.NewRecorder()
			h(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("status %d", w.Code)
			}
		}()
	}
	wg.Wait()
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("upstream calls %d", n)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/instances/sync", nil)
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("upstream calls after cache %d", n)
	}
}

func TestInstancesSyncHandler_Telemetry(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	_, _, _ = initSecrets(t, db)

	var buf bytes.Buffer
	prev := log.Logger
	log.Logger = zerolog.New(logx.NewRedactor(zerolog.SyncWriter(&buf))).With().Timestamp().Logger()
	t.Cleanup(func() { log.Logger = prev })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case "/api/servers":
			fmt.Fprint(w, `{"paging":{"page":1,"size":1,"total":1,"next":""},"servers":[{"id":"1","name":"One"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	if err := pppkg.Set(pppkg.Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("set creds: %v", err)
	}
	h := listServersHandler(db)

	req := httptest.NewRequest(http.MethodPost, "/api/instances/sync", nil)
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	out := buf.String()
	if !strings.Contains(out, "\"event\":\"instances_sync\"") || !strings.Contains(out, "\"status\":\"200\"") || !strings.Contains(out, "\"upstream_status\"") || !strings.Contains(out, "\"deduped\":\"false\"") || !strings.Contains(out, "\"cache_hit\":\"false\"") || !strings.Contains(out, "\"duration_ms\"") {
		t.Fatalf("missing fields: %s", out)
	}

}

func TestInstancesSyncHandler_Upstream403(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	_, _, _ = initSecrets(t, db)
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
	h := listServersHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/instances/sync", nil)
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status %d", w.Code)
	}
	var e httpx.Error
	if err := json.NewDecoder(w.Body).Decode(&e); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Code != "forbidden" || e.Message != "insufficient PufferPanel permissions" {
		t.Fatalf("unexpected error %+v", e)
	}
	if e.RequestID == "" {
		t.Fatal("missing requestId")
	}
}

func TestInstancesSyncHandler_Upstream400(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	_, _, _ = initSecrets(t, db)
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
	h := listServersHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/instances/sync", nil)
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d", w.Code)
	}
	var e httpx.Error
	if err := json.NewDecoder(w.Body).Decode(&e); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Code != "bad_request" || e.Message != "bad request to PufferPanel; check base URL" {
		t.Fatalf("unexpected error %+v", e)
	}
	if e.RequestID == "" {
		t.Fatal("missing requestId")
	}
}

func TestInstancesSyncHandler_Upstream401(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	_, _, _ = initSecrets(t, db)
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
	h := listServersHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/instances/sync", nil)
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", w.Code)
	}
	var e httpx.Error
	if err := json.NewDecoder(w.Body).Decode(&e); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Code != "unauthorized" || e.Message != "invalid PufferPanel credentials" {
		t.Fatalf("unexpected error %+v", e)
	}
	if e.RequestID == "" {
		t.Fatal("missing requestId")
	}
}

func TestInstancesSyncHandler_Upstream5xx(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	_, _, _ = initSecrets(t, db)
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
	h := listServersHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/instances/sync", nil)
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

func TestInstancesSyncHandler_Timeout(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	_, _, _ = initSecrets(t, db)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		case "/api/servers":
			time.Sleep(50 * time.Millisecond)
			fmt.Fprint(w, `{"paging":{"page":0,"size":1,"total":1},"servers":[{"id":"1","name":"One"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	if err := pppkg.Set(pppkg.Credentials{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}); err != nil {
		t.Fatalf("set creds: %v", err)
	}
	h := listServersHandler(db)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/api/instances/sync", nil).WithContext(ctx)
	time.Sleep(2 * time.Millisecond)
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status %d", w.Code)
	}
	var e httpx.Error
	if err := json.NewDecoder(w.Body).Decode(&e); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Code != "bad_gateway" {
		t.Fatalf("unexpected error %+v", e)
	}
}

func TestInstancesSyncHandler_BadConfig(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	_, _, _ = initSecrets(t, db)
	h := listServersHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/instances/sync", nil)
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
	_, _, _ = initSecrets(t, db)
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
	svc := secrets.NewService(db)
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

func TestJobQueue_PerInstanceConcurrency(t *testing.T) {
	origPer, origGlobal := perInstLimit, globalLimit
	perInstLimit = 2
	globalLimit = 10
	defer func() {
		perInstLimit = origPer
		globalLimit = origGlobal
	}()
	db := openTestDB(t)
	defer db.Close()

	origSync := syncFn
	var running, max int32
	syncFn = func(ctx context.Context, w http.ResponseWriter, r *http.Request, db *sql.DB, inst *dbpkg.Instance, serverID string, prog *jobProgress, files []string) {
		n := atomic.AddInt32(&running, 1)
		for {
			m := atomic.LoadInt32(&max)
			if n <= m {
				break
			}
			if atomic.CompareAndSwapInt32(&max, m, n) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt32(&running, -1)
	}
	t.Cleanup(func() { syncFn = origSync })

	inst := &dbpkg.Instance{Name: "i"}
	if err := dbpkg.InsertInstance(db, inst); err != nil {
		t.Fatalf("insert: %v", err)
	}
	ids := []int{}
	for i := 0; i < 5; i++ {
		id, _, err := EnqueueSync(context.Background(), db, inst, "srv", fmt.Sprintf("k%d", i))
		if err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		ids = append(ids, id)
	}
	for _, id := range ids {
		waitJob(t, db, id)
	}
	if max > int32(perInstLimit) {
		t.Fatalf("max concurrency %d", max)
	}
}

func TestJobQueue_GlobalConcurrency(t *testing.T) {
	origPer, origGlobal := perInstLimit, globalLimit
	perInstLimit = 10
	globalLimit = 2
	defer func() {
		perInstLimit = origPer
		globalLimit = origGlobal
	}()
	db := openTestDB(t)
	defer db.Close()

	origSync := syncFn
	var running, max int32
	syncFn = func(ctx context.Context, w http.ResponseWriter, r *http.Request, db *sql.DB, inst *dbpkg.Instance, serverID string, prog *jobProgress, files []string) {
		n := atomic.AddInt32(&running, 1)
		for {
			m := atomic.LoadInt32(&max)
			if n <= m {
				break
			}
			if atomic.CompareAndSwapInt32(&max, m, n) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt32(&running, -1)
	}
	t.Cleanup(func() { syncFn = origSync })

	insts := []*dbpkg.Instance{}
	for i := 0; i < 3; i++ {
		inst := &dbpkg.Instance{Name: fmt.Sprintf("i%d", i)}
		if err := dbpkg.InsertInstance(db, inst); err != nil {
			t.Fatalf("insert: %v", err)
		}
		insts = append(insts, inst)
	}
	ids := []int{}
	for i, inst := range insts {
		id, _, err := EnqueueSync(context.Background(), db, inst, "srv", fmt.Sprintf("k%d", i))
		if err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		ids = append(ids, id)
	}
	for _, id := range ids {
		waitJob(t, db, id)
	}
	if max > int32(globalLimit) {
		t.Fatalf("max concurrency %d", max)
	}
}

func TestJobProgressEndpoint(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	inst := &dbpkg.Instance{Name: "i"}
	if err := dbpkg.InsertInstance(db, inst); err != nil {
		t.Fatalf("insert: %v", err)
	}
	orig := syncFn
	syncFn = func(ctx context.Context, w http.ResponseWriter, r *http.Request, db *sql.DB, inst *dbpkg.Instance, serverID string, prog *jobProgress, files []string) {
		prog.setTotal(2)
		prog.success()
		prog.fail("m", errors.New("boom"))
	}
	id, ch, err := EnqueueSync(context.Background(), db, inst, "srv", "k")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	<-ch
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/jobs/%d", id), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(id))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	jobProgressHandler(db)(w, req)
	syncFn = orig
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var resp struct {
		Total, Processed, Succeeded, Failed, InQueue int
		Failures                                     []jobFailure
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 2 || resp.Processed != 2 || resp.Succeeded != 1 || resp.Failed != 1 || resp.InQueue != 0 {
		t.Fatalf("unexpected progress %+v", resp)
	}
	if len(resp.Failures) != 1 || resp.Failures[0].Name != "m" {
		t.Fatalf("unexpected failures %+v", resp.Failures)
	}
}

func TestCancelJobEndpoint(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	inst := &dbpkg.Instance{Name: "i"}
	if err := dbpkg.InsertInstance(db, inst); err != nil {
		t.Fatalf("insert: %v", err)
	}
	orig := syncFn
	syncFn = func(ctx context.Context, w http.ResponseWriter, r *http.Request, db *sql.DB, inst *dbpkg.Instance, serverID string, prog *jobProgress, files []string) {
		prog.setTotal(100)
		for i := 0; i < 100; i++ {
			if ctx.Err() != nil {
				return
			}
			prog.success()
			time.Sleep(5 * time.Millisecond)
		}
	}
	id, ch, err := EnqueueSync(context.Background(), db, inst, "srv", "k")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		job, err := dbpkg.GetSyncJob(db, id)
		if err != nil {
			t.Fatalf("get job: %v", err)
		}
		if job.Status == JobRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job not running")
		}
		time.Sleep(10 * time.Millisecond)
	}
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/jobs/%d", id), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(id))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	cancelJobHandler(db)(w, req)
	syncFn = orig
	if w.Code != http.StatusNoContent {
		t.Fatalf("status %d", w.Code)
	}
	<-ch
	job, err := dbpkg.GetSyncJob(db, id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != JobCanceled {
		t.Fatalf("status %s", job.Status)
	}
}

func TestJobEventsHandlerStreamsUpdates(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	inst := &dbpkg.Instance{Name: "i"}
	if err := dbpkg.InsertInstance(db, inst); err != nil {
		t.Fatalf("insert: %v", err)
	}
	res, err := db.Exec(`INSERT INTO sync_jobs (instance_id, server_id, status, idempotency_key) VALUES (?,?,?,?)`, inst.ID, "", JobRunning, "k")
	if err != nil {
		t.Fatalf("insert job: %v", err)
	}
	id64, _ := res.LastInsertId()
	id := int(id64)
	jp := newJobProgress()
	jp.setStatus(JobRunning)
	progress.Store(id, jp)

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/events", jobEventsHandler(db))
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(fmt.Sprintf("%s/api/jobs/%d/events", srv.URL, id))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	readEvent := func() string {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if !strings.HasPrefix(line, "data: ") {
			t.Fatalf("unexpected line %q", line)
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		if _, err := reader.ReadString('\n'); err != nil {
			t.Fatalf("read: %v", err)
		}
		return data
	}

	readEvent() // initial snapshot
	jp.setTotal(2)
	_ = readEvent() // after setTotal
	jp.fail("m", errors.New("boom"))
	data := readEvent()
	if !strings.Contains(data, "\"processed\":1") || !strings.Contains(data, "\"failures\"") {
		t.Fatalf("got %s", data)
	}
}

func TestUpdateInstance_LoaderValidator(t *testing.T) {
    db := openTestDB(t)
    defer db.Close()

    // Seed cache with fabric only
    modrinthLoadersMu.Lock()
    modrinthLoadersCache = []metaLoaderOut{{ID: "fabric", Name: "Fabric"}}
    modrinthLoadersExpiry = time.Now().Add(time.Hour)
    modrinthLoadersMu.Unlock()

    // Insert instance
    inst := dbpkg.Instance{Name: "ok", Loader: "", EnforceSameLoader: true}
    if err := dbpkg.InsertInstance(db, &inst); err != nil { t.Fatalf("insert: %v", err) }

    // Reject vanilla
    h := updateInstanceHandler(db)
    req := httptest.NewRequest(http.MethodPut, "/api/instances/"+strconv.Itoa(inst.ID), strings.NewReader(`{"loader":"vanilla"}`))
    rctx := chi.NewRouteContext(); rctx.URLParams.Add("id", strconv.Itoa(inst.ID))
    req = req.WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rctx))
    w := httptest.NewRecorder()
    h(w, req)
    if w.Code != http.StatusBadRequest {
        t.Fatalf("expected 400 for vanilla, got %d", w.Code)
    }

    // Accept cached id
    req2 := httptest.NewRequest(http.MethodPut, "/api/instances/"+strconv.Itoa(inst.ID), strings.NewReader(`{"loader":"fabric"}`))
    req2 = req2.WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rctx))
    w2 := httptest.NewRecorder()
    h(w2, req2)
    if w2.Code != http.StatusOK {
        t.Fatalf("expected 200 for fabric, got %d", w2.Code)
    }
    var updated dbpkg.Instance
    if err := json.NewDecoder(w2.Body).Decode(&updated); err != nil { t.Fatalf("decode: %v", err) }
    if updated.Loader != "fabric" || updated.RequiresLoader {
        t.Fatalf("unexpected instance: %+v", updated)
    }
}

// Ensure per-file state is fresh: two different mods with the same numeric version
// should not reuse slug/version across iterations.
func TestPerformSync_StateIsolationSameVersion(t *testing.T) {
    db := openTestDB(t)
    defer db.Close()

    inst := &dbpkg.Instance{Name: "i", Loader: "fabric"}
    if err := dbpkg.InsertInstance(db, inst); err != nil { t.Fatalf("insert inst: %v", err) }

    // Stub PufferPanel
    origGet := ppGetServer
    origList := ppListPath
    origFetch := ppFetchFile
    ppGetServer = func(ctx context.Context, id string) (*pppkg.ServerDetail, error) {
        return &pppkg.ServerDetail{ID: id, Name: "srv", Environment: struct{ Type string `json:"type"` }{Type: "fabric"}}, nil
    }
    ppListPath = func(ctx context.Context, id, path string) ([]pppkg.FileEntry, error) {
        return []pppkg.FileEntry{{Name: "NoChatReports-1.20.1-fabric.jar"}, {Name: "pandaantispam-1.20.1-fabric.jar"}}, nil
    }
    ppFetchFile = func(ctx context.Context, id, path string) ([]byte, error) { return nil, errors.New("skip") }
    defer func() { ppGetServer = origGet; ppListPath = origList; ppFetchFile = origFetch }()

    // Stub Modrinth client
    old := modClient
    modClient = isoClient{}
    defer func() { modClient = old }()

    // Invoke performSync
    w := httptest.NewRecorder()
    req := httptest.NewRequest(http.MethodPost, "/", nil)
    prog := newJobProgress()
    performSync(context.Background(), w, req, db, inst, "srv", prog, nil)

    if w.Code != 0 && w.Code != http.StatusOK { // http.ResponseWriter may not set code; accept OK/zero
        t.Fatalf("unexpected code %d", w.Code)
    }
    var resp struct{
        Instance dbpkg.Instance `json:"instance"`
        Unmatched []string `json:"unmatched"`
        Mods []dbpkg.Mod `json:"mods"`
    }
    if err := json.NewDecoder(w.Body).Decode(&resp); err != nil { t.Fatalf("decode: %v", err) }
    if len(resp.Mods) != 2 { t.Fatalf("mods len=%d", len(resp.Mods)) }
    urls := map[string]bool{}
    for _, m := range resp.Mods { urls[m.URL] = true }
    if !urls["https://modrinth.com/mod/nochatreports"] || !urls["https://modrinth.com/mod/pandaantispam"] {
        t.Fatalf("unexpected urls: %+v", urls)
    }
}

type isoClient struct{}
func (isoClient) Project(ctx context.Context, slug string) (*mr.Project, error) {
    title := ""
    switch strings.ToLower(slug) {
    case "nochatreports": title = "NoChatReports"
    case "pandaantispam": title = "PandaAntiSpam"
    default: title = slug
    }
    return &mr.Project{Title: title, IconURL: ""}, nil
}
func (isoClient) Versions(ctx context.Context, slug, gameVersion, loader string) ([]mr.Version, error) {
    return []mr.Version{{
        ID: "1", VersionNumber: "1.20.1", VersionType: "release", DatePublished: time.Now(), Loaders: []string{"fabric"}, Files: []mr.VersionFile{{URL: "http://example.com/file.jar"}},
    }}, nil
}
func (isoClient) Resolve(ctx context.Context, slug string) (*mr.Project, string, error) {
    switch strings.ToLower(slug) {
    case "nochatreports": return &mr.Project{Title: "NoChatReports"}, "nochatreports", nil
    case "pandaantispam": return &mr.Project{Title: "PandaAntiSpam"}, "pandaantispam", nil
    default:
        return &mr.Project{Title: slug}, strings.ToLower(slug), nil
    }
}
func (isoClient) Search(ctx context.Context, query string) (*mr.SearchResult, error) {
    // prefer exact title slug mapping
    hits := []struct {
        ProjectID   string `json:"project_id"`
        Slug        string `json:"slug"`
        Title       string `json:"title"`
        Description string `json:"description"`
        IconURL     string `json:"icon_url"`
        Downloads   int    `json:"downloads"`
    }{}
    q := strings.ToLower(query)
    if strings.Contains(q, "nochat") { hits = append(hits, struct{ProjectID string `json:"project_id"`; Slug string `json:"slug"`; Title string `json:"title"`; Description string `json:"description"`; IconURL string `json:"icon_url"`; Downloads int `json:"downloads"`}{"1","nochatreports","NoChatReports","","", 10}) }
    if strings.Contains(q, "panda") { hits = append(hits, struct{ProjectID string `json:"project_id"`; Slug string `json:"slug"`; Title string `json:"title"`; Description string `json:"description"`; IconURL string `json:"icon_url"`; Downloads int `json:"downloads"`}{"2","pandaantispam","PandaAntiSpam","","", 9}) }
    if len(hits) == 0 { hits = append(hits, struct{ProjectID string `json:"project_id"`; Slug string `json:"slug"`; Title string `json:"title"`; Description string `json:"description"`; IconURL string `json:"icon_url"`; Downloads int `json:"downloads"`}{"3", q, query, "", "", 0}) }
    return &mr.SearchResult{Hits: hits}, nil
}
