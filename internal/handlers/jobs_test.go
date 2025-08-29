package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	dbpkg "modsentinel/internal/db"
	"modsentinel/internal/logx"
	"strings"
)

func TestJobQueue_ShutdownWaitsForJobs(t *testing.T) {
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
	stopped := false
	defer func() {
		if !stopped {
			stop(context.Background())
		}
		db.Close()
	}()

	inst := &dbpkg.Instance{Name: "A", Loader: "fabric"}
	if err := dbpkg.InsertInstance(db, inst); err != nil {
		t.Fatalf("insert instance: %v", err)
	}

	old := syncFn
	syncFn = func(ctx context.Context, w http.ResponseWriter, r *http.Request, db *sql.DB, inst *dbpkg.Instance, serverID string, prog *jobProgress, files []string) {
		time.Sleep(100 * time.Millisecond)
	}
	defer func() { syncFn = old }()

	id, _, err := EnqueueSync(context.Background(), db, inst, "", "k1")
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
			t.Fatalf("job did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	start := time.Now()
	stop(context.Background())
	stopped = true
	if time.Since(start) < 80*time.Millisecond {
		t.Fatalf("stop returned too quickly")
	}
	job, err := dbpkg.GetSyncJob(db, id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != JobSucceeded {
		t.Fatalf("got status %s want %s", job.Status, JobSucceeded)
	}
}

func TestEnqueueSync_DedupesByKey(t *testing.T) {
	db := openTestDB(t)
	inst := &dbpkg.Instance{Name: "i"}
	if err := dbpkg.InsertInstance(db, inst); err != nil {
		t.Fatalf("insert: %v", err)
	}
	orig := syncFn
	syncFn = func(ctx context.Context, w http.ResponseWriter, r *http.Request, db *sql.DB, inst *dbpkg.Instance, serverID string, prog *jobProgress, files []string) {
	}
	t.Cleanup(func() { syncFn = orig })
	id1, _, err := EnqueueSync(context.Background(), db, inst, "srv", "key")
	if err != nil {
		t.Fatalf("enqueue1: %v", err)
	}
	id2, _, err := EnqueueSync(context.Background(), db, inst, "srv", "key")
	if err != nil {
		t.Fatalf("enqueue2: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("ids differ %d vs %d", id1, id2)
	}
	var c int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sync_jobs`).Scan(&c); err != nil {
		t.Fatalf("count: %v", err)
	}
	if c != 1 {
		t.Fatalf("got %d jobs want 1", c)
	}
}

func TestQueueMetricsEmitted(t *testing.T) {
	db := openTestDB(t)
	inst := &dbpkg.Instance{Name: "i"}
	if err := dbpkg.InsertInstance(db, inst); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var buf bytes.Buffer
	log.Logger = zerolog.New(logx.NewRedactor(&buf)).With().Timestamp().Logger()
	origSync := syncFn
	syncFn = func(ctx context.Context, w http.ResponseWriter, r *http.Request, db *sql.DB, inst *dbpkg.Instance, serverID string, prog *jobProgress, files []string) {
	}
	id, ch, err := EnqueueSync(context.Background(), db, inst, "", "k")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	// Wait for job to finish to avoid leaking goroutines.
	<-ch
	syncFn = origSync
	if !strings.Contains(buf.String(), "\"event\":\"sync_queue\"") {
		t.Fatalf("expected sync_queue metric, got %s", buf.String())
	}
	// ensure id used to avoid unused var warning
	if id == 0 {
		t.Fatalf("invalid id")
	}
}

func TestRetryFailedEnqueuesOnlyFailures(t *testing.T) {
	db := openTestDB(t)
	inst := &dbpkg.Instance{Name: "i"}
	if err := dbpkg.InsertInstance(db, inst); err != nil {
		t.Fatalf("insert: %v", err)
	}
	res, err := db.Exec(`INSERT INTO sync_jobs(instance_id, server_id, status, idempotency_key) VALUES(?, '', 'succeeded', 'k')`, inst.ID)
	if err != nil {
		t.Fatalf("insert job: %v", err)
	}
	jid64, _ := res.LastInsertId()
	id := int(jid64)
	jp := newJobProgress()
	jp.fail("a", errors.New("boom"))
	jp.fail("b", errors.New("boom"))
	progress.Store(id, jp)
	jobsCh = make(chan int, 1)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/jobs/%d/retry", id), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.Itoa(id))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	retryFailedHandler(db)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	select {
	case gotID := <-jobsCh:
		if gotID != id {
			t.Fatalf("enqueued %d want %d", gotID, id)
		}
	default:
		t.Fatalf("job not enqueued")
	}
	v, ok := retryFiles.Load(id)
	if !ok {
		t.Fatalf("no retry files")
	}
	names := v.([]string)
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Fatalf("got %v", names)
	}
}
