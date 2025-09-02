package handlers

import (
    "context"
    "database/sql"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    urlpkg "net/url"
    "strings"
    "sync"
    "sync/atomic"
    "crypto/sha256"
    "encoding/hex"
    "os"
    "errors"
    "time"
    "strconv"
    
    dbpkg "modsentinel/internal/db"
    pppkg "modsentinel/internal/pufferpanel"
    "modsentinel/internal/telemetry"
)

// withRetry retries fn on transient errors (HTTP 429/5xx for upstream and PufferPanel) with backoff.
func withRetry(ctx context.Context, fn func() error) error {
    base := 200 // ms
    for attempt := 0; attempt < 5; attempt++ {
        if err := fn(); err != nil {
            // detect transient pufferpanel errors
            var pe *pppkg.Error
            if errors.As(err, &pe) {
                if pe.Status == 429 || pe.Status >= 500 {
                    select {
                    case <-ctx.Done():
                        return ctx.Err()
                    case <-time.After(time.Duration(base*(1<<attempt)) * time.Millisecond):
                        continue
                    }
                }
            }
            // network or other retryable
            if ue, ok := err.(interface{ Temporary() bool }); ok && ue.Temporary() {
                select {
                case <-ctx.Done():
                    return ctx.Err()
                case <-time.After(time.Duration(base*(1<<attempt)) * time.Millisecond):
                    continue
                }
            }
            return err
        }
        return nil
    }
    return fmt.Errorf("retry attempts exceeded")
}

// withRetryCount behaves like withRetry but also returns the number of attempts made (>=1).
func withRetryCount(ctx context.Context, fn func() error) (int, error) {
    base := 200 // ms
    for attempt := 0; attempt < 5; attempt++ {
        if err := fn(); err != nil {
            var pe *pppkg.Error
            if errors.As(err, &pe) {
                if pe.Status == 429 || pe.Status >= 500 {
                    select {
                    case <-ctx.Done():
                        return attempt + 1, ctx.Err()
                    case <-time.After(time.Duration(base*(1<<attempt)) * time.Millisecond):
                        continue
                    }
                }
            }
            if ue, ok := err.(interface{ Temporary() bool }); ok && ue.Temporary() {
                select {
                case <-ctx.Done():
                    return attempt + 1, ctx.Err()
                case <-time.After(time.Duration(base*(1<<attempt)) * time.Millisecond):
                    continue
                }
            }
            return attempt + 1, err
        }
        return attempt + 1, nil
    }
    return 5, fmt.Errorf("retry attempts exceeded")
}

type sseMsg struct {
    Event string
    Data  any
}

// UpdateJobState models the lifecycle of a mod update job.
type UpdateJobState string

const (
    StateQueued           UpdateJobState = "Queued"
    StateRunning          UpdateJobState = "Running"
    StateUploadingNew     UpdateJobState = "UploadingNew"
    StateVerifyingNew     UpdateJobState = "VerifyingNew"
    StateRemovingOld      UpdateJobState = "RemovingOld"
    StateVerifyingRemoval UpdateJobState = "VerifyingRemoval"
    StateUpdatingDB       UpdateJobState = "UpdatingDB"
    StateSucceeded        UpdateJobState = "Succeeded"
    StateFailed           UpdateJobState = "Failed"
    StatePartialSuccess   UpdateJobState = "PartialSuccess"
)

type updateJob struct {
    id     int
    mu     sync.Mutex
    events []sseMsg
    subs   map[chan sseMsg]struct{}
    state  UpdateJobState
    db     *sql.DB
    updID  int
}

func (j *updateJob) emit(ev string, data any) {
    j.mu.Lock()
    if j.subs == nil {
        j.subs = make(map[chan sseMsg]struct{})
    }
    msg := sseMsg{Event: ev, Data: data}
    j.events = append(j.events, msg)
    for ch := range j.subs {
        select { case ch <- msg: default: }
    }
    j.mu.Unlock()
}

func (j *updateJob) subscribe() chan sseMsg {
    ch := make(chan sseMsg, 16)
    j.mu.Lock()
    if j.subs == nil { j.subs = make(map[chan sseMsg]struct{}) }
    j.subs[ch] = struct{}{}
    j.mu.Unlock()
    return ch
}

func (j *updateJob) unsubscribe(ch chan sseMsg) {
    j.mu.Lock()
    delete(j.subs, ch)
    close(ch)
    j.mu.Unlock()
}

func (j *updateJob) snapshotEvents() []sseMsg {
    j.mu.Lock()
    defer j.mu.Unlock()
    out := make([]sseMsg, len(j.events))
    copy(out, j.events)
    return out
}

func (j *updateJob) emitState(state UpdateJobState, details map[string]any) {
    j.state = state
    payload := map[string]any{"job_id": j.id, "state": state}
    if details != nil {
        payload["details"] = details
    }
    j.emit("state", payload)
    if j.db != nil && j.updID != 0 {
        switch state {
        case StateRunning:
            _ = dbpkg.MarkModUpdateStarted(j.db, j.updID)
        case StateSucceeded:
            _ = dbpkg.MarkModUpdateFinished(j.db, j.updID, string(state), "")
        case StateFailed:
            var msg string
            if details != nil {
                if v, ok := details["error"].(string); ok {
                    msg = v
                }
            }
            _ = dbpkg.MarkModUpdateFinished(j.db, j.updID, string(state), msg)
        case StatePartialSuccess:
            var msg string
            if details != nil {
                if v, ok := details["hint"].(string); ok { msg = v }
                if v, ok := details["error"].(string); ok && msg == "" { msg = v }
            }
            _ = dbpkg.MarkModUpdateFinished(j.db, j.updID, string(state), msg)
        default:
            _ = dbpkg.UpdateModUpdateStatus(j.db, j.updID, string(state))
        }
    }
}

var (
    updateJobs   sync.Map // map[int]*updateJob
    updateJobSeq atomic.Int64
    updInstMu    sync.Mutex
    updSems      map[int]chan struct{}
    jobIDByUpdID sync.Map      // map[int]jobID
    jobIDByKey   sync.Map      // map[string]jobID
)

func init() {
    // Start from a high range to avoid clashing with DB auto-increment IDs
    updateJobSeq.Store(1_000_000_000)
}

func acquireUpdate(instID int) {
    updInstMu.Lock()
    if updSems == nil {
        updSems = make(map[int]chan struct{})
    }
    sem, ok := updSems[instID]
    if !ok {
        sem = make(chan struct{}, 1)
        updSems[instID] = sem
    }
    updInstMu.Unlock()
    sem <- struct{}{}
}

func releaseUpdate(instID int) {
    updInstMu.Lock()
    sem := updSems[instID]
    select { case <-sem: default: }
    if len(sem) == 0 {
        delete(updSems, instID)
    }
    updInstMu.Unlock()
}

func getUpdateJob(id int) *updateJob {
    if v, ok := updateJobs.Load(id); ok {
        return v.(*updateJob)
    }
    return nil
}

func enqueueUpdateJob(ctx context.Context, db *sql.DB, modID int) (int, error) {
    id := int(updateJobSeq.Add(1))
    // Prepare idempotency info (best-effort)
    prev, _ := dbpkg.GetMod(db, modID)
    key := fmt.Sprintf("%d:%s", modID, strings.TrimSpace(prev.AvailableVersion))
    fromV := prev.CurrentVersion
    toV := prev.AvailableVersion
    updID, existed, err := dbpkg.InsertModUpdateQueued(db, modID, fromV, toV, key)
    if err != nil {
        return 0, err
    }
    if existed {
        if v, ok := jobIDByKey.Load(key); ok {
            return v.(int), nil
        }
        if v, ok := jobIDByUpdID.Load(updID); ok {
            return v.(int), nil
        }
        // fall through to create a new in-memory job wrapper if prior mapping unavailable
    }
    uj := &updateJob{id: id, events: make([]sseMsg, 0, 16), db: db, updID: updID}
    updateJobs.Store(id, uj)
    jobIDByUpdID.Store(updID, id)
    jobIDByKey.Store(key, id)
    uj.emitState(StateQueued, nil)
    go runUpdateJob(ctx, db, uj, modID)
    return id, nil
}

// enqueueUpdateJobWithKey enqueues using a client-supplied idempotency key.
func enqueueUpdateJobWithKey(ctx context.Context, db *sql.DB, modID int, key string) (int, error) {
    id := int(updateJobSeq.Add(1))
    prev, _ := dbpkg.GetMod(db, modID)
    fromV := prev.CurrentVersion
    toV := prev.AvailableVersion
    updID, existed, err := dbpkg.InsertModUpdateQueued(db, modID, fromV, toV, key)
    if err != nil {
        return 0, err
    }
    if existed {
        if v, ok := jobIDByKey.Load(key); ok {
            return v.(int), nil
        }
        if v, ok := jobIDByUpdID.Load(updID); ok {
            return v.(int), nil
        }
        // If job mapping not found, return an error indicating duplicate without active job
        // so client can poll update status by other means if needed.
        return 0, fmt.Errorf("duplicate idempotency_key")
    }
    uj := &updateJob{id: id, events: make([]sseMsg, 0, 16), db: db, updID: updID}
    updateJobs.Store(id, uj)
    jobIDByUpdID.Store(updID, id)
    jobIDByKey.Store(key, id)
    uj.emitState(StateQueued, nil)
    go runUpdateJob(ctx, db, uj, modID)
    return id, nil
}

func runUpdateJob(ctx context.Context, db *sql.DB, uj *updateJob, modID int) {
    defer func() {
        // keep job in memory for clients to reconnect briefly; no purge for now
    }()
    uj.emitState(StateRunning, nil)

    // Load current mod
    prev, err := dbpkg.GetMod(db, modID)
    if err != nil {
        uj.emitState(StateFailed, map[string]any{"error": err.Error()})
        return
    }
    slug, err := parseModrinthSlug(prev.URL)
    if err != nil {
        uj.emitState(StateFailed, map[string]any{"error": "invalid mod URL"})
        return
    }
    if strings.TrimSpace(prev.AvailableVersion) == "" || prev.AvailableVersion == prev.CurrentVersion {
        uj.emitState(StateFailed, map[string]any{"error": "no update available"})
        return
    }
    versions, err := modClient.Versions(ctx, slug, "", "")
    if err != nil {
        // Try to serialize the error
        b, _ := json.Marshal(err)
        uj.emitState(StateFailed, map[string]any{"error": string(b)})
        return
    }
    var targetURL string
    for _, vv := range versions {
        if vv.VersionNumber == prev.AvailableVersion {
            if len(vv.Files) > 0 {
                targetURL = strings.TrimSpace(vv.Files[0].URL)
            }
            break
        }
    }
    if targetURL == "" {
        uj.emitState(StateFailed, map[string]any{"error": "selected update not found"})
        return
    }

    updatedDB := false

    // Upload to PufferPanel first, if configured
    if inst, err2 := dbpkg.GetInstance(db, prev.InstanceID); err2 == nil && strings.TrimSpace(inst.PufferpanelServerID) != "" {
        // Per-instance mutex: prevent concurrent updates on the same server/instance
        acquireUpdate(inst.ID)
        defer releaseUpdate(inst.ID)
        folder := "mods/"
        switch strings.ToLower(inst.Loader) {
        case "paper", "spigot", "bukkit":
            folder = "plugins/"
        }
        deriveName := func(rawURL, slug, defName, version string) string {
            if u, err := urlpkg.Parse(rawURL); err == nil {
                p := u.Path
                if i := strings.LastIndex(p, "/"); i != -1 && i+1 < len(p) {
                    nm := p[i+1:]
                    if nm != "" { return nm }
                }
            }
            base := strings.TrimSpace(slug)
            if base == "" { base = strings.TrimSpace(defName) }
            if base == "" { base = "mod" }
            ver := strings.TrimSpace(version)
            if ver == "" { ver = "latest" }
            return base + "-" + ver + ".jar"
        }
        oldSlug, _ := parseModrinthSlug(prev.URL)
        oldName := deriveName(prev.DownloadURL, oldSlug, prev.Name, prev.CurrentVersion)
        newName := deriveName(targetURL, slug, prev.Name, prev.AvailableVersion)

        // Download artifact
        reqDL, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
        if err != nil {
            uj.emitState(StateFailed, map[string]any{"error": err.Error()})
            return
        }
        var resp *http.Response
        stepStart := time.Now()
        attempts, err := withRetryCount(ctx, func() error {
            var e error
            resp, e = http.DefaultClient.Do(reqDL)
            if e != nil {
                return e
            }
            if resp.StatusCode == 429 || resp.StatusCode >= 500 {
                io.Copy(io.Discard, resp.Body)
                resp.Body.Close()
                return fmt.Errorf("transient upstream %d", resp.StatusCode)
            }
            return nil
        })
        if err != nil {
            uj.emitState(StateFailed, map[string]any{"error": err.Error()})
            return
        }
        defer resp.Body.Close()
        if resp.StatusCode < 200 || resp.StatusCode >= 300 {
            uj.emitState(StateFailed, map[string]any{"error": fmt.Sprintf("download failed: %d", resp.StatusCode)})
            return
        }
        data, err := io.ReadAll(resp.Body)
        if err != nil || len(data) == 0 {
            uj.emitState(StateFailed, map[string]any{"error": "invalid file content"})
            return
        }
        telemetry.Event("mod_update_step", map[string]string{
            "job_id":   strconv.Itoa(uj.id),
            "mod_id":   strconv.Itoa(prev.ID),
            "step":     "Download",
            "ms":       strconv.FormatInt(time.Since(stepStart).Milliseconds(), 10),
            "attempt":  strconv.Itoa(attempts),
            "pp_path":  "",
            "sha256_match": "",
        })
        // compute expected attributes
        expSize := len(data)
        sum := sha256.Sum256(data)
        expSHA := hex.EncodeToString(sum[:])

        uj.emitState(StateUploadingNew, map[string]any{"file": newName, "size": expSize, "sha256": expSHA})
        stepStart = time.Now()
        attempts, err = withRetryCount(ctx, func() error { return pppkg.PutFile(ctx, inst.PufferpanelServerID, folder+newName, data) })
        if err != nil {
            uj.emitState(StateFailed, map[string]any{"error": err.Error()})
            return
        }
        telemetry.Event("mod_update_step", map[string]string{
            "job_id":  strconv.Itoa(uj.id),
            "mod_id":  strconv.Itoa(prev.ID),
            "step":    "UploadingNew",
            "ms":      strconv.FormatInt(time.Since(stepStart).Milliseconds(), 10),
            "attempt": strconv.Itoa(attempts),
            "pp_path": folder + newName,
            "sha256_match": "",
        })
        uj.emitState(StateVerifyingNew, map[string]any{"file": newName, "size": expSize})
        var files []pppkg.FileEntry
        stepStart = time.Now()
        attempts, err = withRetryCount(ctx, func() error {
            var e error
            files, e = ppListPath(ctx, inst.PufferpanelServerID, folder)
            return e
        })
        if err == nil {
            present := false
            for _, f := range files {
                if !f.IsDir && strings.EqualFold(f.Name, newName) { present = true; break }
            }
            if !present {
                uj.emitState(StateFailed, map[string]any{"error": "update verification failed"})
                return
            }
        } else {
            uj.emitState(StateFailed, map[string]any{"error": err.Error()})
            return
        }
        telemetry.Event("mod_update_step", map[string]string{
            "job_id":  strconv.Itoa(uj.id),
            "mod_id":  strconv.Itoa(prev.ID),
            "step":    "VerifyingNewList",
            "ms":      strconv.FormatInt(time.Since(stepStart).Milliseconds(), 10),
            "attempt": strconv.Itoa(attempts),
            "pp_path": folder,
            "sha256_match": "",
        })
        // verify by fetching uploaded file and comparing size and optionally sha256
        verifyHash := strings.EqualFold(os.Getenv("UPDATE_VERIFY_SHA256"), "1") || strings.EqualFold(os.Getenv("UPDATE_VERIFY_SHA256"), "true")
        stepStart = time.Now()
        var b []byte
        attempts, err = withRetryCount(ctx, func() error { var er error; b, er = pppkg.FetchFile(ctx, inst.PufferpanelServerID, folder+newName); return er })
        if err == nil {
            if len(b) != expSize {
                uj.emitState(StateFailed, map[string]any{"error": fmt.Sprintf("size mismatch: expected %d got %d", expSize, len(b))})
                return
            }
            if verifyHash {
                sum2 := sha256.Sum256(b)
                got := hex.EncodeToString(sum2[:])
                if !strings.EqualFold(got, expSHA) {
                    uj.emitState(StateFailed, map[string]any{"error": "sha256 mismatch", "expected": expSHA, "got": got})
                    telemetry.Event("mod_update_step", map[string]string{
                        "job_id":  strconv.Itoa(uj.id),
                        "mod_id":  strconv.Itoa(prev.ID),
                        "step":    "VerifyingNewFetch",
                        "ms":      strconv.FormatInt(time.Since(stepStart).Milliseconds(), 10),
                        "attempt": strconv.Itoa(attempts),
                        "pp_path": folder + newName,
                        "sha256_match": "false",
                    })
                    return
                }
            }
        } else {
            uj.emitState(StateFailed, map[string]any{"error": err.Error()})
            return
        }
        telemetry.Event("mod_update_step", map[string]string{
            "job_id":  strconv.Itoa(uj.id),
            "mod_id":  strconv.Itoa(prev.ID),
            "step":    "VerifyingNewFetch",
            "ms":      strconv.FormatInt(time.Since(stepStart).Milliseconds(), 10),
            "attempt": strconv.Itoa(attempts),
            "pp_path": folder + newName,
            "sha256_match": func() string { if verifyHash { return "true" }; return "" }(),
        })
        // Update DB now that the new file is present
        uj.emitState(StateUpdatingDB, map[string]any{"mod_id": prev.ID})
        stepStart = time.Now()
        if _, err := db.Exec(`UPDATE mods SET current_version=?, channel=?, download_url=? WHERE id=?`, prev.AvailableVersion, prev.AvailableChannel, targetURL, prev.ID); err != nil {
            // Rollback: attempt to remove the newly uploaded file to keep old files
            _ = withRetry(ctx, func() error { return pppkg.DeleteFile(ctx, inst.PufferpanelServerID, folder+newName) })
            uj.emitState(StateFailed, map[string]any{"error": err.Error(), "hint": "DB update failed after upload; new file removed. Please retry later."})
            telemetry.Event("mod_update_failed", map[string]string{
                "job_id": strconv.Itoa(uj.id),
                "mod_id": strconv.Itoa(prev.ID),
                "error":  err.Error(),
            })
            return
        }
        updatedDB = true
        telemetry.Event("mod_update_step", map[string]string{
            "job_id":  strconv.Itoa(uj.id),
            "mod_id":  strconv.Itoa(prev.ID),
            "step":    "UpdatingDB",
            "ms":      strconv.FormatInt(time.Since(stepStart).Milliseconds(), 10),
            "attempt": "1",
            "pp_path": "",
            "sha256_match": "",
        })

        // Remove old file; on failure mark partial success and stop
        uj.emitState(StateRemovingOld, map[string]any{"file": oldName})
        var delErr error
        stepStart = time.Now()
        attempts, err = withRetryCount(ctx, func() error {
            var e error
            files, e = ppListPath(ctx, inst.PufferpanelServerID, folder)
            return e
        })
        if err == nil {
            for _, f := range files {
                if !f.IsDir && strings.EqualFold(f.Name, oldName) {
                    _, delErr = withRetryCount(ctx, func() error { return pppkg.DeleteFile(ctx, inst.PufferpanelServerID, folder+oldName) })
                    break
                }
            }
        } else {
            _, delErr = withRetryCount(ctx, func() error { return pppkg.DeleteFile(ctx, inst.PufferpanelServerID, folder+oldName) })
        }
        if delErr != nil {
            uj.emitState(StatePartialSuccess, map[string]any{"file": oldName, "hint": "Old file could not be removed; please delete it manually from the server."})
            telemetry.Event("mod_update_step", map[string]string{
                "job_id":  strconv.Itoa(uj.id),
                "mod_id":  strconv.Itoa(prev.ID),
                "step":    "RemovingOld",
                "ms":      strconv.FormatInt(time.Since(stepStart).Milliseconds(), 10),
                "attempt": strconv.Itoa(attempts),
                "pp_path": folder + oldName,
                "sha256_match": "",
            })
            telemetry.Event("mod_update_failed", map[string]string{
                "job_id": strconv.Itoa(uj.id),
                "mod_id": strconv.Itoa(prev.ID),
                "error":  "delete_old_failed",
            })
            return
        }
        telemetry.Event("mod_update_step", map[string]string{
            "job_id":  strconv.Itoa(uj.id),
            "mod_id":  strconv.Itoa(prev.ID),
            "step":    "RemovingOld",
            "ms":      strconv.FormatInt(time.Since(stepStart).Milliseconds(), 10),
            "attempt": strconv.Itoa(attempts),
            "pp_path": folder + oldName,
            "sha256_match": "",
        })
        // Verify removal; if still present, partial success
        removed := true
        stepStart = time.Now()
        attempts, err = withRetryCount(ctx, func() error {
            var e error
            files, e = ppListPath(ctx, inst.PufferpanelServerID, folder)
            return e
        })
        if err == nil {
            for _, f := range files {
                if !f.IsDir && strings.EqualFold(f.Name, oldName) { removed = false; break }
            }
        }
        uj.emitState(StateVerifyingRemoval, map[string]any{"file": oldName, "removed": removed})
        telemetry.Event("mod_update_step", map[string]string{
            "job_id":  strconv.Itoa(uj.id),
            "mod_id":  strconv.Itoa(prev.ID),
            "step":    "VerifyingRemoval",
            "ms":      strconv.FormatInt(time.Since(stepStart).Milliseconds(), 10),
            "attempt": strconv.Itoa(attempts),
            "pp_path": folder,
            "sha256_match": "",
        })
        if !removed {
            uj.emitState(StatePartialSuccess, map[string]any{"file": oldName, "hint": "Old file still present; please delete it manually from the server."})
            telemetry.Event("mod_update_failed", map[string]string{
                "job_id": strconv.Itoa(uj.id),
                "mod_id": strconv.Itoa(prev.ID),
                "error":  "verify_removal_failed",
            })
            return
        }
    }

    // If DB not updated in PufferPanel path (no server configured), update it now
    if !updatedDB {
        uj.emitState(StateUpdatingDB, map[string]any{"mod_id": prev.ID})
        if _, err := db.Exec(`UPDATE mods SET current_version=?, channel=?, download_url=? WHERE id=?`, prev.AvailableVersion, prev.AvailableChannel, targetURL, prev.ID); err != nil {
            uj.emitState(StateFailed, map[string]any{"error": err.Error(), "hint": "DB update failed."})
            return
        }
    }
    _ = dbpkg.InsertUpdateIfNew(db, prev.ID, prev.AvailableVersion)
    m, err := dbpkg.GetMod(db, prev.ID)
    if err != nil {
        uj.emitState(StateFailed, map[string]any{"error": err.Error()})
        return
    }
    _ = dbpkg.InsertEvent(db, &dbpkg.ModEvent{InstanceID: m.InstanceID, ModID: &m.ID, Action: "updated", ModName: m.Name, From: prev.CurrentVersion, To: m.CurrentVersion})
    uj.emitState(StateSucceeded, map[string]any{"mod_id": m.ID, "version": m.CurrentVersion})
}
