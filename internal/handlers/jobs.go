package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"

	dbpkg "modsentinel/internal/db"
	"modsentinel/internal/telemetry"
)

const (
	JobQueued    = "queued"
	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"
	JobCanceled  = "canceled"
)

var (
	jobsCh  chan int
	waiters sync.Map // map[int]chan struct{}
	jobDB   *sql.DB

	perInstLimit = 4
	globalLimit  = 16

	instMu    sync.Mutex
	instSems  map[int]chan struct{}
	globalSem chan struct{}

	syncFn func(ctx context.Context, w http.ResponseWriter, r *http.Request, db *sql.DB, inst *dbpkg.Instance, serverID string, prog *jobProgress, files []string) = performSync

	runWg  sync.WaitGroup
	active int64

	jobCancels sync.Map // map[int]context.CancelFunc
	progress   sync.Map // map[int]*jobProgress
	retryFiles sync.Map // map[int][]string
)

const maxFailures = 5

type jobFailure struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

type jobProgress struct {
	mu        sync.Mutex
	total     int
	processed int
	succeeded int
	failed    int
	status    string
	failures  []jobFailure
	subs      map[chan struct{}]struct{}
}

func (p *jobProgress) setTotal(n int) {
	p.mu.Lock()
	p.total = n
	p.notifyLocked()
	p.mu.Unlock()
}

func (p *jobProgress) success() {
	p.mu.Lock()
	p.processed++
	p.succeeded++
	p.notifyLocked()
	p.mu.Unlock()
}

func (p *jobProgress) fail(name string, err error) {
	p.mu.Lock()
	p.processed++
	p.failed++
	if len(p.failures) >= maxFailures {
		copy(p.failures, p.failures[1:])
		p.failures = p.failures[:maxFailures-1]
	}
	p.failures = append(p.failures, jobFailure{Name: name, Error: err.Error()})
	p.notifyLocked()
	p.mu.Unlock()
}

func (p *jobProgress) setStatus(s string) {
	p.mu.Lock()
	p.status = s
	p.notifyLocked()
	p.mu.Unlock()
}

func (p *jobProgress) subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	p.mu.Lock()
	if p.subs == nil {
		p.subs = make(map[chan struct{}]struct{})
	}
	p.subs[ch] = struct{}{}
	p.mu.Unlock()
	return ch
}

func (p *jobProgress) unsubscribe(ch chan struct{}) {
	p.mu.Lock()
	delete(p.subs, ch)
	p.mu.Unlock()
}

func (p *jobProgress) notifyLocked() {
	for ch := range p.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (p *jobProgress) snapshot() (total, processed, succeeded, failed int, fails []jobFailure, status string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fails = append([]jobFailure(nil), p.failures...)
	return p.total, p.processed, p.succeeded, p.failed, fails, p.status
}

func newJobProgress() *jobProgress {
	return &jobProgress{subs: make(map[chan struct{}]struct{}), failures: make([]jobFailure, 0, maxFailures)}
}

func recordQueueMetrics() {
	telemetry.Event("sync_queue", map[string]string{
		"depth":  strconv.Itoa(len(jobsCh)),
		"active": strconv.FormatInt(atomic.LoadInt64(&active), 10),
	})
}

// StartJobQueue launches the background worker and enqueues pending jobs.
// It returns a shutdown function that waits for in-flight jobs to finish or
// requeue them if the provided context is canceled while waiting.
func StartJobQueue(ctx context.Context, db *sql.DB) func(context.Context) {
	jobDB = db
	jobsCh = make(chan int, 16)
	instSems = make(map[int]chan struct{})
	globalSem = make(chan struct{}, globalLimit)
	runCtx, cancel := context.WithCancel(ctx)
	runWg.Add(1)
	go worker(runCtx)
	if err := dbpkg.ResetRunningSyncJobs(db); err == nil {
		ids, err := dbpkg.ListQueuedSyncJobs(db)
		if err == nil {
			for _, id := range ids {
				p := newJobProgress()
				p.setStatus(JobQueued)
				progress.Store(id, p)
				jobsCh <- id
			}
		}
	}
	return func(waitCtx context.Context) {
		cancel()
		close(jobsCh)
		done := make(chan struct{})
		go func() {
			runWg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-waitCtx.Done():
			_ = dbpkg.ResetRunningSyncJobs(jobDB)
		}
	}
}

// EnqueueSync schedules a sync job for the given instance/server.
// Duplicate requests with the same idempotency key return the existing job.
func EnqueueSync(ctx context.Context, db *sql.DB, inst *dbpkg.Instance, serverID, key string) (int, <-chan struct{}, error) {
	id, existed, err := dbpkg.InsertSyncJob(db, inst.ID, serverID, key)
	if err != nil {
		return 0, nil, err
	}
	if existed {
		if ch, ok := waiters.Load(id); ok {
			return id, ch.(chan struct{}), nil
		}
		ch := make(chan struct{})
		waiters.Store(id, ch)
		return id, ch, nil
	}
	ch := make(chan struct{})
	waiters.Store(id, ch)
	p := newJobProgress()
	p.setStatus(JobQueued)
	progress.Store(id, p)
	jobsCh <- id
	recordQueueMetrics()
	return id, ch, nil
}

func worker(ctx context.Context) {
	defer runWg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case id, ok := <-jobsCh:
			if !ok {
				return
			}
			recordQueueMetrics()
			job, err := dbpkg.GetSyncJob(jobDB, id)
			if err != nil {
				_ = dbpkg.MarkSyncJobFinished(jobDB, id, JobFailed, err.Error())
				if ch, ok := waiters.Load(id); ok {
					close(ch.(chan struct{}))
					waiters.Delete(id)
				}
				continue
			}
			if job.Status != JobQueued {
				if ch, ok := waiters.Load(id); ok {
					close(ch.(chan struct{}))
					waiters.Delete(id)
				}
				continue
			}
			runWg.Add(1)
			go func(job *dbpkg.SyncJob) {
				defer runWg.Done()
				acquire(job.InstanceID)
				atomic.AddInt64(&active, 1)
				recordQueueMetrics()
				defer func() {
					atomic.AddInt64(&active, -1)
					recordQueueMetrics()
				}()
				defer release(job.InstanceID)
				runJob(ctx, job)
			}(job)
		}
	}
}

func acquire(instID int) {
	globalSem <- struct{}{}
	instMu.Lock()
	sem, ok := instSems[instID]
	if !ok {
		sem = make(chan struct{}, perInstLimit)
		instSems[instID] = sem
	}
	instMu.Unlock()
	sem <- struct{}{}
}

func release(instID int) {
	instMu.Lock()
	sem := instSems[instID]
	<-sem
	if len(sem) == 0 {
		delete(instSems, instID)
	}
	instMu.Unlock()
	<-globalSem
}

func runJob(ctx context.Context, job *dbpkg.SyncJob) {
	_ = dbpkg.MarkSyncJobRunning(jobDB, job.ID)
	inst, err := dbpkg.GetInstance(jobDB, job.InstanceID)
	if err != nil {
		_ = dbpkg.MarkSyncJobFinished(jobDB, job.ID, JobFailed, err.Error())
		if ch, ok := waiters.Load(job.ID); ok {
			close(ch.(chan struct{}))
			waiters.Delete(job.ID)
		}
		return
	}
	baseCtx := context.WithoutCancel(ctx)
	jobCtx, cancel := context.WithCancel(baseCtx)
	jobCancels.Store(job.ID, cancel)
	defer jobCancels.Delete(job.ID)
	jw := &jobWriter{}
	req := &http.Request{Method: http.MethodPost, URL: &url.URL{Path: "/"}, Header: make(http.Header)}
	req = req.WithContext(jobCtx)
	p, _ := progress.LoadOrStore(job.ID, newJobProgress())
	jp := p.(*jobProgress)
	jp.setStatus(JobRunning)
	var names []string
	if v, ok := retryFiles.Load(job.ID); ok {
		names = v.([]string)
		retryFiles.Delete(job.ID)
	}
	syncFn(jobCtx, jw, req, jobDB, inst, job.ServerID, jp, names)
	status := JobSucceeded
	errMsg := ""
	switch {
	case jobCtx.Err() != nil:
		status = JobCanceled
	case jw.status >= 400:
		status = JobFailed
		errMsg = jw.buf.String()
	}
	_ = dbpkg.MarkSyncJobFinished(jobDB, job.ID, status, errMsg)
	jp.setStatus(status)
	if ch, ok := waiters.Load(job.ID); ok {
		close(ch.(chan struct{}))
		waiters.Delete(job.ID)
	}
}

type jobWriter struct {
	header http.Header
	status int
	buf    bytes.Buffer
}

func (jw *jobWriter) Header() http.Header {
	if jw.header == nil {
		jw.header = make(http.Header)
	}
	return jw.header
}

func (jw *jobWriter) Write(b []byte) (int, error) {
	if jw.status == 0 {
		jw.status = http.StatusOK
	}
	return jw.buf.Write(b)
}

func (jw *jobWriter) WriteHeader(code int) {
	jw.status = code
}
