package handlers

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	urlpkg "net/url"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	singleflight "golang.org/x/sync/singleflight"
	rate "golang.org/x/time/rate"
	dbpkg "modsentinel/internal/db"
	"modsentinel/internal/httpx"
	mr "modsentinel/internal/modrinth"
	pppkg "modsentinel/internal/pufferpanel"
	"modsentinel/internal/secrets"
	"modsentinel/internal/telemetry"
	tokenpkg "modsentinel/internal/token"
)

type modrinthClient interface {
    Project(ctx context.Context, slug string) (*mr.Project, error)
    Versions(ctx context.Context, slug, gameVersion, loader string) ([]mr.Version, error)
    Resolve(ctx context.Context, slug string) (*mr.Project, string, error)
    Search(ctx context.Context, query string) (*mr.SearchResult, error)
}

var modClient modrinthClient = mr.NewClient()

// allow tests to stub PufferPanel interactions
var (
    ppGetServer = pppkg.GetServer
    ppListPath  = pppkg.ListPath
    ppFetchFile = pppkg.FetchFile
    // fetch template definition and data
    ppGetServerDefinition = pppkg.GetServerDefinition
    ppGetServerDefinitionRaw = pppkg.GetServerDefinitionRaw
    ppGetServerData       = pppkg.GetServerData
)

// guardedVersions wraps modrinth Versions to avoid sending misleading constraints.
// - If the provided loader is not a valid Modrinth loader (per cached tags), we drop
//   both loader and gameVersion filters to prevent mixed signals.
// - If the loader is valid, we pass through both loader and gameVersion.
// This keeps the Modrinth loader cache as the source of truth for valid IDs.
func guardedVersions(ctx context.Context, slug, gameVersion, loader string) ([]mr.Version, error) {
    if !isValidLoader(ctx, loader) {
        return modClient.Versions(ctx, slug, "", "")
    }
    return modClient.Versions(ctx, slug, gameVersion, loader)
}

var lastSync atomic.Int64
var latencyP50 atomic.Int64
var latencyP95 atomic.Int64
var resyncAliasHits atomic.Int64 // counts deprecated /resync alias hits
// ALLOW_RESYNC_ALIAS gates the deprecated /resync alias.
// TODO: remove this flag and alias after 2025-01-01.
var allowResyncAlias = func() bool {
	v := strings.ToLower(os.Getenv("ALLOW_RESYNC_ALIAS"))
	return v == "" || v == "1" || v == "true"
}()

var latencyMu sync.Mutex
var latencySamples []int64

var writeLimiter = rate.NewLimiter(rate.Every(time.Second), 5)

var csrfToken string

var (
	listServersTTL   = 2 * time.Second
	listServersSF    singleflight.Group
	listServersCache sync.Map // map[baseURL]listServersEntry
)

// Cache for Modrinth loader tags
var (
    modrinthLoadersTTL    = 24 * time.Hour
    modrinthLoadersMu     sync.RWMutex
    modrinthLoadersCache  []metaLoaderOut
    modrinthLoadersExpiry time.Time
)

// autoDetectableLoaders enumerates loader ids considered safe for automatic matching.
var autoDetectableLoaders = map[string]struct{}{
	"bukkit":       {},
	"datapack":     {},
	"fabric":       {},
	"forge":        {},
	"neoforge":     {},
	"paper":        {},
	"resourcepack": {},
	"spigot":       {},
	"quilt":        {},
}

type listServersEntry struct {
	servers []pppkg.Server
	exp     time.Time
}

type nonceCtxKey struct{}

func init() {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	csrfToken = base64.StdEncoding.EncodeToString(b)
}

// CSRFToken returns the server CSRF token. Exposed for tests.
func CSRFToken() string { return csrfToken }

func writeModrinthError(w http.ResponseWriter, r *http.Request, err error) {
	var me *mr.Error
	if errors.As(err, &me) && (me.Status == http.StatusUnauthorized || me.Status == http.StatusForbidden) {
		httpx.Write(w, r, httpx.Unauthorized("token required"))
		return
	}
	httpx.Write(w, r, httpx.BadRequest(err.Error()))
}

func writePPError(w http.ResponseWriter, r *http.Request, err error) int {
	var ce *pppkg.ConfigError
	if errors.As(err, &ce) {
		httpx.Write(w, r, httpx.BadRequest(ce.Error()))
		return http.StatusBadRequest
	}
	if errors.Is(err, pppkg.ErrForbidden) {
		httpx.Write(w, r, httpx.Forbidden("insufficient PufferPanel permissions"))
		return http.StatusForbidden
	}
	if errors.Is(err, pppkg.ErrNotFound) {
		http.NotFound(w, r)
		return http.StatusNotFound
	}
	var pe *pppkg.Error
	if errors.As(err, &pe) {
		switch {
		case pe.Status == http.StatusBadRequest:
			httpx.Write(w, r, httpx.BadRequest("bad request to PufferPanel; check base URL"))
			return http.StatusBadRequest
		case pe.Status == http.StatusUnauthorized:
			httpx.Write(w, r, httpx.Unauthorized("invalid PufferPanel credentials"))
			return http.StatusUnauthorized
		case pe.Status == http.StatusForbidden:
			httpx.Write(w, r, httpx.Forbidden("insufficient PufferPanel permissions"))
			return http.StatusForbidden
		case pe.Status >= 500:
			httpx.Write(w, r, httpx.BadGateway(pe.Error()))
			return http.StatusBadGateway
		default:
			httpx.Write(w, r, httpx.BadGateway(pe.Error()))
			return http.StatusBadGateway
		}
	}
	httpx.Write(w, r, httpx.BadGateway(err.Error()))
	return http.StatusBadGateway
}

type metadataRequest struct {
	URL string `json:"url" validate:"required,url"`
}

func validatePayload(v interface{}) *httpx.HTTPError {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	rt := rv.Type()
	fields := map[string]string{}
	for i := 0; i < rv.NumField(); i++ {
		field := rv.Field(i)
		sf := rt.Field(i)
		tag := sf.Tag.Get("validate")
		if tag == "" {
			continue
		}
		name := strings.ToLower(sf.Name)
		if jsonTag := sf.Tag.Get("json"); jsonTag != "" {
			name = strings.Split(jsonTag, ",")[0]
		}
		for _, rule := range strings.Split(tag, ",") {
			switch rule {
			case "required":
				if field.IsZero() {
					fields[name] = "required"
				}
			case "url":
				if _, ok := fields[name]; ok {
					continue
				}
				s, ok := field.Interface().(string)
				if !ok || s == "" {
					fields[name] = "url"
					continue
				}
				if _, err := urlpkg.ParseRequestURI(s); err != nil {
					fields[name] = "url"
				}
			}
		}
	}
	if len(fields) > 0 {
		return httpx.BadRequest("validation failed").WithDetails(fields)
	}
	return nil
}



type tokenRequest struct {
	Token string `json:"token" validate:"required"`
}

type pufferRequest struct {
	BaseURL      string `json:"base_url" validate:"required,url"`
	ClientID     string `json:"client_id" validate:"required"`
	ClientSecret string `json:"client_secret" validate:"required"`
	Scopes       string `json:"scopes"`
	DeepScan     bool   `json:"deep_scan"`
}

func setSecretHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !writeLimiter.Allow() {
			httpx.Write(w, r, httpx.TooManyRequests("rate limit exceeded"))
			return
		}
		typ := chi.URLParam(r, "type")
		var last4 string
		switch typ {
		case "modrinth":
			var req tokenRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				httpx.Write(w, r, httpx.BadRequest("invalid json"))
				return
			}
			if err := validatePayload(&req); err != nil {
				httpx.Write(w, r, err)
				return
			}
			if n := len(req.Token); n > 4 {
				last4 = req.Token[n-4:]
			} else {
				last4 = req.Token
			}
			if err := tokenpkg.SetToken(req.Token); err != nil {
				httpx.Write(w, r, httpx.Internal(err))
				return
			}
		case "pufferpanel":
			var req pufferRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				httpx.Write(w, r, httpx.BadRequest("invalid json"))
				return
			}
			if err := validatePayload(&req); err != nil {
				httpx.Write(w, r, err)
				return
			}
			if n := len(req.ClientSecret); n > 4 {
				last4 = req.ClientSecret[n-4:]
			} else {
				last4 = req.ClientSecret
			}
			creds := pppkg.Credentials{BaseURL: req.BaseURL, ClientID: req.ClientID, ClientSecret: req.ClientSecret, Scopes: req.Scopes, DeepScan: req.DeepScan}
			if err := pppkg.TestConnection(r.Context(), creds); err != nil {
				writePPError(w, r, err)
				return
			}
			if err := pppkg.Set(creds); err != nil {
				httpx.Write(w, r, httpx.Internal(err))
				return
			}
		default:
			httpx.Write(w, r, httpx.BadRequest("unknown secret type"))
			return
		}
		telemetry.Event("secret_set", map[string]string{"type": typ})
		log.Info().Str("type", typ).Str("last4", last4).Msg("secret set")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}

func deleteSecretHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !writeLimiter.Allow() {
			httpx.Write(w, r, httpx.TooManyRequests("rate limit exceeded"))
			return
		}
		typ := chi.URLParam(r, "type")
		var err error
		switch typ {
		case "modrinth":
			err = tokenpkg.ClearToken()
		case "pufferpanel":
			err = pppkg.Clear()
		default:
			httpx.Write(w, r, httpx.BadRequest("unknown secret type"))
			return
		}
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		telemetry.Event("secret_cleared", map[string]string{"type": typ})
		log.Info().Str("type", typ).Msg("secret deleted")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}

func secretStatusHandler(svc *secrets.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		typ := chi.URLParam(r, "type")
		var (
			exists    bool
			last4     string
			updatedAt time.Time
			err       error
		)
		if typ == "pufferpanel" {
			exists, last4, updatedAt, err = svc.Status(r.Context(), "puffer.oauth_client_secret")
		} else {
			exists, last4, updatedAt, err = svc.Status(r.Context(), typ)
		}
		if err != nil {
			if exists {
				httpx.Write(w, r, httpx.NotFound("secret invalid"))
			} else {
				httpx.Write(w, r, httpx.Internal(err))
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(map[string]any{
			"exists":     exists,
			"last4":      last4,
			"updated_at": updatedAt,
		})
		log.Info().Str("type", typ).Str("last4", last4).Msg("secret status")
	}
}

func testPufferHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		creds, err := pppkg.Get()
		if err != nil {
			writePPError(w, r, err)
			return
		}
		if creds == (pppkg.Credentials{}) {
			httpx.Write(w, r, httpx.BadRequest("credentials not configured"))
			return
		}
		if err := pppkg.TestConnection(r.Context(), creds); err != nil {
			writePPError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func listServersHandler(db *sql.DB) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		status := http.StatusOK
		cacheHit := false
		deduped := false
		upstreamStatus := 0
		defer func() {
			telemetry.Event("instances_sync", map[string]string{
				"status":          strconv.Itoa(status),
				"duration_ms":     strconv.FormatInt(time.Since(start).Milliseconds(), 10),
				"deduped":         strconv.FormatBool(deduped),
				"cache_hit":       strconv.FormatBool(cacheHit),
				"upstream_status": strconv.Itoa(upstreamStatus),
			})
		}()

		creds, err := pppkg.Config()
		if err != nil {
			status = writePPError(w, r, err)
			return
		}
		var servers []pppkg.Server
		if v, ok := listServersCache.Load(creds.BaseURL); ok {
			ent := v.(listServersEntry)
			if time.Now().Before(ent.exp) {
				cacheHit = true
				servers = ent.servers
			}
		}
            if servers == nil {
                var shared bool
                var v any
                v, err, shared = listServersSF.Do(creds.BaseURL, func() (any, error) {
                    svs, us, err := pppkg.ListServersWithStatus(r.Context())
                    upstreamStatus = us
                    if err != nil {
                        return nil, err
                    }
                    return svs, nil
                })
                deduped = shared
                if err != nil {
                    status = writePPError(w, r, err)
                    return
                }
                servers = v.([]pppkg.Server)
                listServersCache.Store(creds.BaseURL, listServersEntry{servers: servers, exp: time.Now().Add(listServersTTL)})
            }
            w.Header().Set("Content-Type", "application/json")
            json.NewEncoder(w).Encode(servers)
        }
}

func syncHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/resync") {
			if !allowResyncAlias {
				w.WriteHeader(http.StatusGone)
				return
			}
			hits := resyncAliasHits.Add(1)
			telemetry.Event("instances_sync_alias", map[string]string{
				"path_alias": "resync",
				"hits":       strconv.FormatInt(hits, 10),
			})
			log.Warn().Str("path", r.URL.Path).Str("path_alias", "resync").Int64("alias_hits", hits).Msg("/api/instances/{id}/resync is deprecated; use /sync instead")
		}
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		inst, err := dbpkg.GetInstance(db, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		var req struct {
			ServerID string `json:"serverId"`
			Key      string `json:"key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			if !errors.Is(err, io.EOF) {
				httpx.Write(w, r, httpx.BadRequest("invalid json"))
				return
			}
		}
		serverID := req.ServerID
		if serverID == "" {
			serverID = inst.PufferpanelServerID
		}
		if serverID == "" {
			httpx.Write(w, r, httpx.BadRequest("validation failed").WithDetails(map[string]string{"serverId": "required"}))
			return
		}
		key := req.Key
		if key == "" {
			key = uuid.NewString()
		}
		jobID, _, err := EnqueueSync(r.Context(), db, inst, serverID, key)
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			ID     int    `json:"id"`
			Status string `json:"status"`
		}{jobID, JobQueued})
		return
	}
}

func jobProgressHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        idStr := chi.URLParam(r, "id")
        id, err := strconv.Atoi(idStr)
        if err != nil {
            httpx.Write(w, r, httpx.NotFound("job not found"))
            return
        }
        job, err := dbpkg.GetSyncJob(db, id)
        if err != nil {
            // Fallback: in-memory update job
            if uj := getUpdateJob(id); uj != nil {
                evs := uj.snapshotEvents()
                var last any
                if len(evs) > 0 {
                    last = evs[len(evs)-1].Data
                }
                w.Header().Set("Content-Type", "application/json")
                json.NewEncoder(w).Encode(struct {
                    ID      int         `json:"id"`
                    State   string      `json:"state"`
                    Details interface{} `json:"details,omitempty"`
                }{id, string(uj.state), last})
                return
            }
            httpx.Write(w, r, httpx.NotFound("job not found"))
            return
        }
		var total, processed, succeeded, failed int
		var fails []jobFailure
		if jp, ok := progress.Load(id); ok {
			total, processed, succeeded, failed, fails, _ = jp.(*jobProgress).snapshot()
		}
		inQueue := total - processed
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			ID        int          `json:"id"`
			Status    string       `json:"status"`
			Total     int          `json:"total"`
			Processed int          `json:"processed"`
			Succeeded int          `json:"succeeded"`
			Failed    int          `json:"failed"`
			InQueue   int          `json:"in_queue"`
			Failures  []jobFailure `json:"failures"`
		}{job.ID, job.Status, total, processed, succeeded, failed, inQueue, fails})
	}
}

func jobEventsHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        idStr := chi.URLParam(r, "id")
        id, err := strconv.Atoi(idStr)
        if err != nil {
            httpx.Write(w, r, httpx.NotFound("job not found"))
            return
        }
        if _, err := dbpkg.GetSyncJob(db, id); err != nil {
            // If not a sync job, try in-memory update job stream
            if uj := getUpdateJob(id); uj != nil {
                flusher, ok := w.(http.Flusher)
                if !ok {
                    http.Error(w, "stream unsupported", http.StatusInternalServerError)
                    return
                }
                w.Header().Set("Content-Type", "text/event-stream")
                w.Header().Set("Cache-Control", "no-cache")
                w.Header().Set("Connection", "keep-alive")
                ch := uj.subscribe()
                defer uj.unsubscribe(ch)
                // replay existing events
                for _, ev := range uj.snapshotEvents() {
                    if ev.Event != "" {
                        fmt.Fprintf(w, "event: %s\n", ev.Event)
                    }
                    if ev.Data != nil {
                        b, _ := json.Marshal(ev.Data)
                        fmt.Fprintf(w, "data: %s\n\n", b)
                    } else {
                        fmt.Fprintf(w, "data: {}\n\n")
                    }
                }
                flusher.Flush()
                for {
                    select {
                    case <-r.Context().Done():
                        return
                    case ev := <-ch:
                        if ev.Event != "" {
                            fmt.Fprintf(w, "event: %s\n", ev.Event)
                        }
                        if ev.Data != nil {
                            b, _ := json.Marshal(ev.Data)
                            fmt.Fprintf(w, "data: %s\n\n", b)
                        } else {
                            fmt.Fprintf(w, "data: {}\n\n")
                        }
                        flusher.Flush()
                        if ev.Event == "succeeded" || ev.Event == "failed" {
                            return
                        }
                    }
                }
            }
            httpx.Write(w, r, httpx.NotFound("job not found"))
            return
        }
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "stream unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		p, _ := progress.LoadOrStore(id, newJobProgress())
		jp := p.(*jobProgress)
		ch := jp.subscribe()
		defer jp.unsubscribe(ch)

		send := func() bool {
			total, processed, succeeded, failed, fails, status := jp.snapshot()
			inQueue := total - processed
			data, _ := json.Marshal(struct {
				ID        int          `json:"id"`
				Status    string       `json:"status"`
				Total     int          `json:"total"`
				Processed int          `json:"processed"`
				Succeeded int          `json:"succeeded"`
				Failed    int          `json:"failed"`
				InQueue   int          `json:"in_queue"`
				Failures  []jobFailure `json:"failures"`
			}{id, status, total, processed, succeeded, failed, inQueue, fails})
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return false
			}
			flusher.Flush()
			switch status {
			case JobSucceeded, JobFailed, JobCanceled:
				return false
			}
			return true
		}

		if !send() {
			return
		}
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ch:
				if !send() {
					return
				}
			}
		}
	}
}

func cancelJobHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		job, err := dbpkg.GetSyncJob(db, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		switch job.Status {
		case JobQueued:
			_ = dbpkg.MarkSyncJobFinished(db, id, JobCanceled, "")
			if ch, ok := waiters.Load(id); ok {
				close(ch.(chan struct{}))
				waiters.Delete(id)
			}
		case JobRunning:
			if c, ok := jobCancels.Load(id); ok {
				c.(context.CancelFunc)()
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func retryFailedHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		job, err := dbpkg.GetSyncJob(db, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if job.Status == JobQueued || job.Status == JobRunning {
			http.Error(w, "job active", http.StatusConflict)
			return
		}
		p, ok := progress.Load(id)
		if !ok {
			http.Error(w, "no failures", http.StatusBadRequest)
			return
		}
		_, _, _, _, fails, _ := p.(*jobProgress).snapshot()
		if len(fails) == 0 {
			http.Error(w, "no failures", http.StatusBadRequest)
			return
		}
		names := make([]string, len(fails))
		for i, f := range fails {
			names[i] = f.Name
		}
		if err := dbpkg.RequeueSyncJob(db, id); err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		np := newJobProgress()
		np.setStatus(JobQueued)
		np.setTotal(len(names))
		progress.Store(id, np)
		retryFiles.Store(id, names)
		ch := make(chan struct{})
		waiters.Store(id, ch)
		jobsCh <- id
		recordQueueMetrics()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			ID int `json:"id"`
		}{id})
	}
}

func performSync(ctx context.Context, w http.ResponseWriter, r *http.Request, db *sql.DB, inst *dbpkg.Instance, serverID string, prog *jobProgress, only []string) {
    _, err := ppGetServer(ctx, serverID)
    if err != nil {
        writePPError(w, r, err)
        return
    }
    inst.PufferpanelServerID = serverID
    if err := validatePayload(inst); err != nil {
        httpx.Write(w, r, err)
        return
    }
    // Derive loader from template definition and environment
    // Priority:
    // 1) Any display strings containing a known loader token (normalized)
    // 2) install[] hints for Fabric
    // 3) run.command content
    // 4) Fallback: requires_loader=true
    _ = ensureModrinthLoaders(ctx)
    normalize := func(s string) string {
        s = strings.ToLower(strings.TrimSpace(s))
        s = strings.ReplaceAll(s, " ", "")
        s = strings.ReplaceAll(s, "-", "")
        s = strings.ReplaceAll(s, "_", "")
        return s
    }
    // Build token->id map from Modrinth loader cache
    tokens := map[string]string{}
    modrinthLoadersMu.RLock()
    for _, t := range modrinthLoadersCache {
        if strings.TrimSpace(t.ID) == "" { continue }
        id := strings.ToLower(t.ID)
        if _, ok := autoDetectableLoaders[id]; !ok {
            continue
        }
        // base tokens: id and lowercased name
        tokens[normalize(id)] = id
        if strings.TrimSpace(t.Name) != "" {
            tokens[normalize(t.Name)] = id
        }
        // minimal aliases when obvious, only if the canonical id exists in cache
        switch id {
        case "fabric":
            tokens[normalize("fabricdl")] = id
        case "neoforge":
            // support dashed alias often seen in displays/installers
            tokens[normalize("neo-forge")] = id
        }
    }
    modrinthLoadersMu.RUnlock()
    findInText := func(s string) string {
        ns := normalize(s)
        if ns == "" { return "" }
        for tok, id := range tokens {
            if tok == "" { continue }
            if strings.Contains(ns, tok) { return id }
        }
        return ""
    }
    requiresLoader := false
    detected := ""
    source := ""
    envDisplay := ""
    topDisplay := ""
    // Load definition (raw) and structured (for variables)
    var def *pppkg.ServerDefinition
    defFetched := 0
    // Log: start fetching definition
    log.Ctx(ctx).Info().
        Int("instance_id", inst.ID).
        Str("server_id", serverID).
        Msg("definition_fetch_start")
    if d, err := ppGetServerDefinition(ctx, serverID); err == nil {
        def = d
        defFetched++
    }
    defRaw := map[string]any{}
    if raw, err := ppGetServerDefinitionRaw(ctx, serverID); err == nil {
        defRaw = raw
        defFetched++
    }
    // Log: fetched definition summary
    {
        disp := ""
        if v, ok := defRaw["display"].(string); ok { disp = v }
        typ := ""
        if v, ok := defRaw["type"].(string); ok { typ = v }
        envType := ""
        if env, ok := defRaw["environment"].(map[string]any); ok {
            if v, ok2 := env["type"].(string); ok2 { envType = v }
        }
        log.Ctx(ctx).Info().
            Int("instance_id", inst.ID).
            Str("server_id", serverID).
            Str("display", disp).
            Str("type", typ).
            Str("env_type", envType).
            Int("definitions_fetched", defFetched).
            Msg("definition_fetch_ok")
    }
    // 1) Primary: display fields
    // - top-level display (if present)
    if disp, ok := defRaw["display"].(string); ok {
        topDisplay = disp
        if id := findInText(disp); id != "" { detected = id; source = "display" }
    }
    // - environment.display (if present)
    if envRaw, ok := defRaw["environment"].(map[string]any); ok {
        if disp, ok2 := envRaw["display"].(string); ok2 {
            envDisplay = disp
            if id := findInText(disp); id != "" { detected = id; source = "display" }
        }
    }
    // - variable displays from definition data
    if detected == "" && def != nil && def.Data != nil {
        for _, v := range def.Data {
            d := strings.TrimSpace(v.Display)
            if d == "" { continue }
            if id := findInText(d); id != "" { detected = id; source = "display" }
            if detected != "" { break }
        }
    }
    // Build a lowercase haystack from display, type, environment.type, install[], run.command
    var dispParts []string
    if topDisplay != "" { dispParts = append(dispParts, strings.ToLower(topDisplay)) }
    if envDisplay != "" { dispParts = append(dispParts, strings.ToLower(envDisplay)) }
    // include variable displays
    if def != nil && def.Data != nil {
        for _, v := range def.Data { if strings.TrimSpace(v.Display) != "" { dispParts = append(dispParts, strings.ToLower(v.Display)) } }
    }
    // types
    var typeParts []string
    if t, ok := defRaw["type"].(string); ok { typeParts = append(typeParts, strings.ToLower(t)) }
    if envRaw, ok := defRaw["environment"].(map[string]any); ok {
        if t, ok2 := envRaw["type"].(string); ok2 { typeParts = append(typeParts, strings.ToLower(t)) }
    }
    // install[]
    var instTypeParts, instCmdParts, instMoveParts []string
    if instArr, ok := defRaw["install"].([]any); ok {
        for _, it := range instArr {
            step, _ := it.(map[string]any)
            if step == nil { continue }
            if typ, _ := step["type"].(string); typ != "" { instTypeParts = append(instTypeParts, strings.ToLower(typ)) }
            if cmdStr, ok2 := step["commands"].(string); ok2 && strings.TrimSpace(cmdStr) != "" {
                instCmdParts = append(instCmdParts, strings.ToLower(cmdStr))
            } else if cmdArr, ok2 := step["commands"].([]any); ok2 {
                for _, c := range cmdArr { if s, ok3 := c.(string); ok3 && strings.TrimSpace(s) != "" { instCmdParts = append(instCmdParts, strings.ToLower(s)) } }
            }
            if mvArr, ok2 := step["moves"].([]any); ok2 {
                for _, m := range mvArr {
                    if mm, ok3 := m.(map[string]any); ok3 {
                        if v, ok4 := mm["target"].(string); ok4 && strings.TrimSpace(v) != "" { instMoveParts = append(instMoveParts, strings.ToLower(v)) }
                        if v, ok4 := mm["to"].(string); ok4 && strings.TrimSpace(v) != "" { instMoveParts = append(instMoveParts, strings.ToLower(v)) }
                    }
                }
            }
        }
    }
    // run.command
    runCmdLower := ""
    if dataRC, errRC := ppFetchFile(ctx, serverID, "run.command"); errRC == nil {
        runCmdLower = strings.ToLower(string(dataRC))
    }
    // Combine haystack
    hayLower := strings.Join(append(append(append(dispParts, typeParts...), instTypeParts...), append(instCmdParts, append(instMoveParts, runCmdLower)...)...), "\n")
    hayFlat := normalize(hayLower)
    // Scan tokens in descending length and collect all distinct loader hits.
    conflict := false
    if len(tokens) > 0 {
        keys := make([]string, 0, len(tokens))
        for k := range tokens { keys = append(keys, k) }
        sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
        seen := map[string]struct{}{}
        srcFor := map[string]string{}
        for _, k := range keys {
            if k == "" { continue }
            if strings.Contains(hayFlat, k) || strings.Contains(hayLower, k) {
                id := tokens[k]
                if _, ok := seen[id]; !ok {
                    // best-effort source attribution for the first time we see this id
                    dispFlat := normalize(strings.Join(dispParts, "\n"))
                    iTypeFlat := normalize(strings.Join(instTypeParts, "\n"))
                    iCmdFlat := normalize(strings.Join(instCmdParts, "\n"))
                    iMoveFlat := normalize(strings.Join(instMoveParts, "\n"))
                    switch {
                    case strings.Contains(dispFlat, k):
                        srcFor[id] = "display"
                    case strings.Contains(iTypeFlat, k):
                        srcFor[id] = "install.type"
                    case strings.Contains(iCmdFlat, k):
                        srcFor[id] = "install.command"
                    case strings.Contains(iMoveFlat, k):
                        srcFor[id] = "install.move"
                    case strings.Contains(normalize(runCmdLower), k):
                        srcFor[id] = "run.command"
                    default:
                        srcFor[id] = "display"
                    }
                }
                seen[id] = struct{}{}
            }
        }
        switch len(seen) {
        case 0:
            // keep detected empty
        case 1:
            for id := range seen { detected = id; source = srcFor[id] }
        default:
            // conflicting evidence, treat as unknown
            conflict = true
            detected = ""
        }
    }
    // Telemetry: record autoset or unknown with reasons
    if detected != "" {
        telemetry.Event("loader_autoset", map[string]string{
            "instance_id": strconv.Itoa(inst.ID),
            "id":          detected,
            "source":      source,
        })
        log.Ctx(ctx).Info().
            Int("instance_id", inst.ID).
            Str("server_id", serverID).
            Str("loader", detected).
            Str("source", source).
            Msg("loader_autoset")
    } else {
        // Build reasons for unknown result
        reasons := make([]string, 0, 4)
        if strings.TrimSpace(topDisplay) == "" && strings.TrimSpace(envDisplay) == "" {
            reasons = append(reasons, "no_display")
        } else {
            reasons = append(reasons, "no_display_token")
        }
        if conflict { reasons = append(reasons, "conflict") }
        hasInstallHint := len(instTypeParts) > 0 || len(instCmdParts) > 0 || len(instMoveParts) > 0
        if !hasInstallHint {
            reasons = append(reasons, "no_install_hint")
        }
        if strings.TrimSpace(runCmdLower) == "" {
            reasons = append(reasons, "no_run_command")
        } else {
            reasons = append(reasons, "no_run_command_hint")
        }
        if defFetched == 0 {
            reasons = append(reasons, "no_definition")
        }
        telemetry.Event("loader_autoset", map[string]string{
            "instance_id": strconv.Itoa(inst.ID),
            "result":      "unknown",
            "reasons":     strings.Join(reasons, ","),
        })
        log.Ctx(ctx).Warn().
            Int("instance_id", inst.ID).
            Str("server_id", serverID).
            Str("result", "unknown").
            Str("reasons", strings.Join(reasons, ",")).
            Msg("loader_autoset")
    }
    // Sanity metric: definition fetches per sync
    telemetry.Event("definitions_fetched_per_sync", map[string]string{"count": strconv.Itoa(defFetched)})
    if defFetched == 0 {
        log.Ctx(ctx).Warn().Int("instance_id", inst.ID).Str("server_id", serverID).Msg("no definitions fetched during sync")
    }
    // 4) Decide final loader flags
    // If unknown and no loader is currently set, mark requires_loader.
    // If a loader is already set (either user-set or previously known),
    // do not flip requires_loader back to true on detection failure.
    // If detected, update loader in-memory for this sync and persist later.
    var loaderParam any = nil
    if detected == "" {
        if strings.TrimSpace(inst.Loader) == "" {
            requiresLoader = true
        } else {
            // Keep existing loader and ensure UI remains unblocked
            requiresLoader = false
        }
        // leave inst.Loader unchanged
    } else {
        inst.Loader = detected
        loaderParam = detected
        requiresLoader = false
    }
    // Try to detect game version from PufferPanel server definition/data; best-effort.
    var detectedKey, detectedVal string
    if def, err1 := ppGetServerDefinition(ctx, serverID); err1 == nil {
        if data, err2 := ppGetServerData(ctx, serverID); err2 == nil {
            if k, v, ok := detectGameVersion(def, data); ok {
                detectedKey, detectedVal = k, v
            }
            // Adjacent version capture: if data["game-version"].value exists, store it
            if vw, ok := data.Data["game-version"]; ok && vw.Value != nil {
                var vStr string
                switch x := vw.Value.(type) {
                case string:
                    vStr = strings.TrimSpace(x)
                default:
                    b, _ := json.Marshal(x)
                    vStr = strings.Trim(string(b), `"`)
                }
                vStr = strings.TrimSpace(vStr)
                if vStr != "" {
                    // Only apply if detectGameVersion didn't already pick something, preserving manual version rules
                    if detectedKey == "" || detectedVal == "" {
                        if inst.PufferVersionKey == "game-version" {
                            detectedKey, detectedVal = "game-version", vStr
                        } else if inst.PufferVersionKey == "" && strings.TrimSpace(inst.GameVersion) == "" {
                            detectedKey, detectedVal = "game-version", vStr
                        }
                    }
                }
            }
        } else {
            // Fallback: some templates include current values inside definition.data[].value
            // Try to read game-version directly from raw definition when /data endpoint is unavailable
            if rawData, ok := defRaw["data"].(map[string]any); ok {
                if meta, ok2 := rawData["game-version"].(map[string]any); ok2 {
                    if vv, ok3 := meta["value"]; ok3 && vv != nil {
                        var vStr string
                        switch x := vv.(type) {
                        case string:
                            vStr = strings.TrimSpace(x)
                        default:
                            b, _ := json.Marshal(x)
                            vStr = strings.Trim(string(b), `"`)
                        }
                        if vStr != "" {
                            if inst.PufferVersionKey == "game-version" {
                                detectedKey, detectedVal = "game-version", vStr
                            } else if inst.PufferVersionKey == "" && strings.TrimSpace(inst.GameVersion) == "" {
                                detectedKey, detectedVal = "game-version", vStr
                            }
                        }
                    }
                }
            }
        }
    }
    // Update version based on rules:
    // - If the detected key matches previously stored key, update value.
    // - If there was no previously detected key AND no stored version, set key and value.
    // - Do not overwrite a manual version (version set but no key).
    var keyParam any = nil
    var valParam any = nil
    if detectedKey != "" && detectedVal != "" {
        if inst.PufferVersionKey == detectedKey {
            // Same key, value may have changed; update only value
            valParam = detectedVal
        } else if inst.PufferVersionKey == "" && strings.TrimSpace(inst.GameVersion) == "" {
            // Previously unknown; set both key and value
            keyParam = detectedKey
            valParam = detectedVal
        }
    }
    if _, err := db.Exec(`UPDATE instances SET loader=COALESCE(?, loader), requires_loader=?, pufferpanel_server_id=?, puffer_version_key=COALESCE(?, puffer_version_key), game_version=COALESCE(?, game_version) WHERE id=?`, loaderParam, requiresLoader, inst.PufferpanelServerID, keyParam, valParam, inst.ID); err != nil {
        httpx.Write(w, r, httpx.Internal(err))
        return
    }
    if !requiresLoader && loaderParam != nil {
        telemetry.Event("loader_set", map[string]string{
            "source":      "autoset",
            "loader":      inst.Loader,
            "instance_id": strconv.Itoa(inst.ID),
        })
    }
    // Gate further actions if loader could not be determined
    if requiresLoader {
        // In queued/periodic sync path (jobWriter), skip mod resolution but
        // allow non-mod metadata refresh to persist; log telemetry.
        if _, isJob := w.(*jobWriter); isJob {
            telemetry.Event("sync_skip", map[string]string{
                "reason":      "loader_required",
                "instance_id": strconv.Itoa(inst.ID),
            })
            return
        }
        // For interactive/manual HTTP path, surface 409 so the UI can prompt to set loader.
        telemetry.Event("action_blocked", map[string]string{"action": "sync", "reason": "loader_required", "instance_id": strconv.Itoa(inst.ID)})
        httpx.Write(w, r, httpx.LoaderRequired())
        return
    }
    folder := "mods/"
	switch inst.Loader {
	case "paper", "spigot":
		folder = "plugins/"
	}
	var files []string
	if len(only) > 0 {
		files = append([]string(nil), only...)
	} else {
		entries, err := ppListPath(ctx, serverID, folder)
		if err != nil {
			if errors.Is(err, pppkg.ErrNotFound) {
				msg := strings.TrimSuffix(folder, "/") + " folder missing"
				httpx.Write(w, r, httpx.NotFound(msg))
				return
			}
			writePPError(w, r, err)
			return
		}
		files = make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir {
				continue
			}
			if strings.HasSuffix(strings.ToLower(e.Name), ".jar") {
				files = append(files, e.Name)
			}
		}
		sort.Strings(files)
	}
    prog.setTotal(len(files))
    matched := make([]dbpkg.Mod, 0)
    // Track discovered canonical URLs from server to drive deletions
    discovered := make(map[string]struct{})
    // Basic counts
    var addedCount, updatedCount int
    log.Debug().
        Int("instance_id", inst.ID).
        Str("server_id", serverID).
        Str("loader", inst.Loader).
        Int("files_count", len(files)).
        Msg("starting sync scan")

    // Build a quick lookup of existing mods by canonical URL to avoid duplicates
    existingMods, err := dbpkg.ListMods(db, inst.ID)
    if err != nil {
        httpx.Write(w, r, httpx.Internal(err))
        return
    }
    existingByURL := make(map[string]dbpkg.Mod, len(existingMods))
    for _, em := range existingMods {
        existingByURL[strings.TrimSpace(strings.ToLower(em.URL))] = em
    }
	unmatched := make([]string, 0, len(files))

    for _, f := range files {
        if ctx.Err() != nil {
            return
        }
        meta := parseJarFilename(f)
        slug, ver := meta.Slug, meta.Version
        detectedLoader := ""
        // Prefer metadata in jar over filename when available
        if data, err := ppFetchFile(ctx, serverID, folder+f); err == nil {
            if s2, v2, l2 := parseJarMetadata(data); s2 != "" || v2 != "" || l2 != "" {
                if s2 != "" { slug = s2 }
                if v2 != "" { ver = v2 }
                if ml := mapLoader(l2); ml != "" { detectedLoader = ml }
            }
        }
        // Build candidate alias key
        base := strings.TrimSuffix(strings.ToLower(f), ".jar")
        cand := meta.Slug
        if cand == "" { cand = base }
        cand = normalizeCandidate(cand)
        // Check alias map first to avoid repeated searches
        if cand != "" {
            if mapped, ok, _ := dbpkg.GetAlias(db, inst.ID, cand); ok && mapped != "" {
                slug = mapped
            }
        }
        scanned := false
        if slug == "" || ver == "" {
            scanned = true
            time.Sleep(100 * time.Millisecond)
            data, err := ppFetchFile(ctx, serverID, folder+f)
            if err == nil {
                s2, v2, l2 := parseJarMetadata(data)
                if s2 != "" { slug = s2 }
                if v2 != "" { ver = v2 }
                if ml := mapLoader(l2); ml != "" { detectedLoader = ml }
            }
        }
               if slug == "" || ver == "" {
                        unmatched = append(unmatched, f)
                        prog.fail(f, errors.New("missing slug or version"))
                        log.Debug().
                            Int("instance_id", inst.ID).
                            Str("server_id", serverID).
                            Str("file", f).
                            Bool("deep_scanned", scanned).
                            Msg("modrinth match failed: missing slug or version")
                        if slug != "" {
                                _ = dbpkg.SetModSyncState(db, inst.ID, slug, ver, JobFailed)
                        }
                        continue
               }
               // Resolve canonical slug and remember alias on success
               proj, slug, err := modClient.Resolve(ctx, slug)
               if err != nil && !scanned {
                    time.Sleep(100 * time.Millisecond)
                    data, err2 := ppFetchFile(ctx, serverID, folder+f)
                    if err2 == nil {
                        s2, v2, l2 := parseJarMetadata(data)
                        if s2 != "" { slug = s2 }
                        if v2 != "" { ver = v2 }
                        if ml := mapLoader(l2); ml != "" { detectedLoader = ml }
                        proj, slug, err = modClient.Resolve(ctx, slug)
                    }
                }
               if err != nil {
                        if ctx.Err() != nil {
                                return
                        }
                        unmatched = append(unmatched, f)
                        prog.fail(f, err)
                        log.Debug().
                            Int("instance_id", inst.ID).
                            Str("server_id", serverID).
                            Str("file", f).
                            Str("slug", slug).
                            Str("version", ver).
                            Err(err).
                            Msg("modrinth resolve failed")
                        _ = dbpkg.SetModSyncState(db, inst.ID, slug, ver, JobFailed)
                        continue
               }
               // Remember alias mapping for future runs
               if cand != "" && slug != "" { _ = dbpkg.SetAlias(db, inst.ID, cand, slug) }
        versions, err := modClient.Versions(ctx, slug, "", "")
        if err != nil {
            if ctx.Err() != nil {
                return
            }
            unmatched = append(unmatched, f)
            prog.fail(f, err)
            log.Debug().
                Int("instance_id", inst.ID).
                Str("server_id", serverID).
                Str("file", f).
                Str("slug", slug).
                Str("version", ver).
                Err(err).
                Msg("modrinth versions fetch failed")
            _ = dbpkg.SetModSyncState(db, inst.ID, slug, ver, JobFailed)
            continue
        }
        var v mr.Version
        found := false
        // First try normalized exact version match
        verNorm := normalizeVersion(ver)
        for _, vv := range versions {
            if normalizeVersion(vv.VersionNumber) == verNorm {
                v = vv
                found = true
                break
            }
        }
               if !found {
                        // Attempt: deep scan if not already done
                        if !scanned {
                            time.Sleep(100 * time.Millisecond)
                            if data, err2 := pppkg.FetchFile(ctx, serverID, folder+f); err2 == nil {
                                if s2, v2, l2 := parseJarMetadata(data); s2 != "" || v2 != "" || l2 != "" {
                                    if s2 != "" { slug = s2 }
                                    if v2 != "" { ver = v2 }
                                    if l2 != "" { detectedLoader = l2 }
                                    if proj2, slug2, err2 := modClient.Resolve(ctx, slug); err2 == nil {
                                        proj = proj2
                                        slug = slug2
                                        if vers2, err3 := modClient.Versions(ctx, slug, "", ""); err3 == nil {
                                            verNorm = normalizeVersion(ver)
                                            for _, vv := range vers2 {
                                                if normalizeVersion(vv.VersionNumber) == verNorm { v = vv; found = true; break }
                                            }
                                        }
                                    }
                                }
                            }
                        }
                        // Fallback: search by normalized filename and try hits
                        if !found {
                            query := meta.Slug
                            if strings.TrimSpace(query) == "" { query = strings.TrimSuffix(f, ".jar") }
                            query = normalizeCandidate(query)
                            if res, errS := modClient.Search(ctx, query); errS == nil && len(res.Hits) > 0 {
                                tried := 0
                                for _, hit := range res.Hits {
                                    tried++
                                    if tried > 10 { break }
                                    if vers3, errV := modClient.Versions(ctx, hit.Slug, "", ""); errV == nil {
                                        // First try normalized exact
                                        for _, vv := range vers3 {
                                            if normalizeVersion(vv.VersionNumber) == verNorm {
                                                if proj3, errP := modClient.Project(ctx, hit.Slug); errP == nil {
                                                    proj = proj3
                                                    slug = hit.Slug
                                                    if cand != "" { _ = dbpkg.SetAlias(db, inst.ID, cand, slug) }
                                                    v = vv
                                                    found = true
                                                }
                                                break
                                            }
                                        }
                                        // Then heuristic newest with filename similarity and loader
                                        if !found {
                                            var best mr.Version
                                            var bestTime time.Time
                                            nameTokens := tokenizeFilename(f)
                                            // Build candidates prioritizing instance loader, then filename loader, then detected loader
                                            preferred := mapLoader(inst.Loader)
                                            fileHint := mapLoader(meta.Loader)
                                            candidates := vers3
                                            // Helper to filter by a specific loader id
                                            filterBy := func(list []mr.Version, want string) []mr.Version {
                                                if strings.TrimSpace(want) == "" { return nil }
                                                out := make([]mr.Version, 0, len(list))
                                                for _, x := range list {
                                                    if len(x.Loaders) == 0 { out = append(out, x); continue }
                                                    okL := false
                                                    for _, ld := range x.Loaders { if mapLoader(ld) == want { okL = true; break } }
                                                    if okL { out = append(out, x) }
                                                }
                                                return out
                                            }
                                            if pl := strings.TrimSpace(preferred); pl != "" {
                                                if flt := filterBy(candidates, pl); len(flt) > 0 { candidates = flt }
                                            }
                                            if candidates == nil || len(candidates) == 0 {
                                                if fl := strings.TrimSpace(fileHint); fl != "" {
                                                    if flt := filterBy(vers3, fl); len(flt) > 0 { candidates = flt }
                                                }
                                            }
                                            if (candidates == nil || len(candidates) == 0) && strings.TrimSpace(detectedLoader) != "" {
                                                if flt := filterBy(vers3, detectedLoader); len(flt) > 0 { candidates = flt }
                                            }
                                            if candidates == nil || len(candidates) == 0 { candidates = vers3 }
                                            for _, vv := range candidates {
                                                sim := 0.0
                                                if len(vv.Files) > 0 {
                                                    b := basenameFromURL(vv.Files[0].URL)
                                                    sim = jaccard(nameTokens, tokenizeFilename(b))
                                                }
                                                if sim < 0.3 { continue }
                                                if vv.DatePublished.After(bestTime) { best = vv; bestTime = vv.DatePublished }
                                            }
                                            if best.ID != "" {
                                                if proj3, errP := modClient.Project(ctx, hit.Slug); errP == nil {
                                                    proj = proj3
                                                    slug = hit.Slug
                                                    if cand != "" { _ = dbpkg.SetAlias(db, inst.ID, cand, slug) }
                                                    v = best
                                                    found = true
                                                }
                                            }
                                        }
                                    }
                                    if found { break }
                                }
                            }
                        }
                        if !found {
                            unmatched = append(unmatched, f)
                            prog.fail(f, fmt.Errorf("version %s not found", ver))
                            log.Debug().
                                Int("instance_id", inst.ID).
                                Str("server_id", serverID).
                                Str("file", f).
                                Str("slug", slug).
                                Str("version", ver).
                                Msg("modrinth match failed: version not found")
                            _ = dbpkg.SetModSyncState(db, inst.ID, slug, ver, JobFailed)
                            continue
                        }
               }
        m := dbpkg.Mod{
            Name:           proj.Title,
            IconURL:        proj.IconURL,
            URL:            fmt.Sprintf("https://modrinth.com/mod/%s", slug),
            InstanceID:     inst.ID,
            Channel:        strings.ToLower(v.VersionType),
            CurrentVersion: v.VersionNumber,
        }
		if len(v.GameVersions) > 0 {
			m.GameVersion = v.GameVersions[0]
		}
        // Choose loader for the mod record
        // Always prioritize the instance-selected loader when set; otherwise fall back
        if pl := mapLoader(inst.Loader); pl != "" {
            m.Loader = pl
        } else if detectedLoader != "" {
            m.Loader = detectedLoader
        } else if len(v.Loaders) > 0 {
            // Map the first supported loader from version metadata, ignoring "minecraft"
            chosen := ""
            for _, ld := range v.Loaders { if ml := mapLoader(ld); ml != "" { chosen = ml; break } }
            m.Loader = chosen
        }
        if len(v.Files) > 0 {
            m.DownloadURL = v.Files[0].URL
        }
               if err := populateAvailableVersion(ctx, &m, slug); err != nil {
                        if ctx.Err() != nil {
                                return
                        }
                        unmatched = append(unmatched, f)
                        prog.fail(f, err)
                        _ = dbpkg.SetModSyncState(db, inst.ID, slug, ver, JobFailed)
                        continue
               }
               // Deduplicate by canonical URL per instance. Update existing instead of inserting.
               key := strings.TrimSpace(strings.ToLower(m.URL))
               discovered[key] = struct{}{}
               if prev, ok := existingByURL[key]; ok {
                        // Update fields if changed to reflect current scan
                        m.ID = prev.ID
                        if prev.Name != m.Name || prev.IconURL != m.IconURL || prev.GameVersion != m.GameVersion || prev.Loader != m.Loader || prev.Channel != m.Channel || prev.CurrentVersion != m.CurrentVersion || prev.AvailableVersion != m.AvailableVersion || prev.AvailableChannel != m.AvailableChannel || prev.DownloadURL != m.DownloadURL {
                                if err := dbpkg.UpdateMod(db, &m); err != nil {
                                        if ctx.Err() != nil {
                                                return
                                        }
                                        unmatched = append(unmatched, f)
                                        prog.fail(f, err)
                                        _ = dbpkg.SetModSyncState(db, inst.ID, slug, ver, JobFailed)
                                        continue
                                }
                                if prev.CurrentVersion != m.CurrentVersion {
                                    _ = dbpkg.InsertEvent(db, &dbpkg.ModEvent{InstanceID: m.InstanceID, ModID: &m.ID, Action: "updated", ModName: m.Name, From: prev.CurrentVersion, To: m.CurrentVersion})
                                }
                                updatedCount++
                                log.Debug().
                                    Int("instance_id", inst.ID).
                                    Str("server_id", serverID).
                                    Str("file", f).
                                    Str("slug", slug).
                                    Str("name", m.Name).
                                    Str("version", m.CurrentVersion).
                                    Str("loader", m.Loader).
                                    Msg("updated mod for instance")
                        }
                        matched = append(matched, m)
                        prog.success()
                        _ = dbpkg.SetModSyncState(db, inst.ID, slug, ver, JobSucceeded)
                        continue
               }
               if err := dbpkg.InsertMod(db, &m); err != nil {
                        if ctx.Err() != nil {
                                return
                        }
                        unmatched = append(unmatched, f)
                        prog.fail(f, err)
                        _ = dbpkg.SetModSyncState(db, inst.ID, slug, ver, JobFailed)
                        continue
               }
               // Track newly inserted so subsequent duplicates in same run update instead of reinsert
               existingByURL[key] = m
               discovered[key] = struct{}{}
               addedCount++
               _ = dbpkg.InsertEvent(db, &dbpkg.ModEvent{InstanceID: m.InstanceID, ModID: &m.ID, Action: "added", ModName: m.Name, To: m.CurrentVersion})
               log.Debug().
                    Int("instance_id", inst.ID).
                    Str("server_id", serverID).
                    Str("file", f).
                    Str("slug", slug).
                    Str("name", m.Name).
                    Str("version", m.CurrentVersion).
                    Str("loader", m.Loader).
                    Msg("added mod to instance")
               matched = append(matched, m)
               prog.success()
               _ = dbpkg.SetModSyncState(db, inst.ID, slug, ver, JobSucceeded)
    }
    // Build a quick set of existing jar filenames for presence checks
    fileSet := make(map[string]struct{}, len(files))
    for _, name := range files { fileSet[strings.ToLower(name)] = struct{}{} }
    // Delete mods from DB that have no corresponding jar on the server
    for _, em := range existingMods {
        // Candidates: basename of download_url, or slug-currentVersion.jar
        candidates := []string{}
        if u, err := urlpkg.Parse(em.DownloadURL); err == nil {
            if p := u.Path; p != "" {
                if i := strings.LastIndex(p, "/"); i != -1 && i+1 < len(p) {
                    if nm := p[i+1:]; nm != "" { candidates = append(candidates, strings.ToLower(nm)) }
                }
            }
        }
        if slug, err := parseModrinthSlug(em.URL); err == nil {
            base := strings.TrimSpace(slug)
            if base == "" { base = strings.TrimSpace(em.Name) }
            if base == "" { base = "mod" }
            ver := strings.TrimSpace(em.CurrentVersion)
            if ver == "" { ver = "latest" }
            candidates = append(candidates, strings.ToLower(base+"-"+ver+".jar"))
        }
        present := false
        for _, c := range candidates {
            if _, ok := fileSet[c]; ok { present = true; break }
        }
        if !present {
            _ = dbpkg.DeleteMod(db, em.ID)
            _ = dbpkg.InsertEvent(db, &dbpkg.ModEvent{InstanceID: em.InstanceID, ModID: &em.ID, Action: "deleted", ModName: em.Name, From: em.CurrentVersion})
            updatedCount++ // treat deletions as instance changes for sync stats
        }
    }
    if err := dbpkg.UpdateInstanceSync(db, inst.ID, addedCount, updatedCount, len(unmatched)); err != nil {
        httpx.Write(w, r, httpx.Internal(err))
        return
    }
    inst2, err := dbpkg.GetInstance(db, inst.ID)
    if err != nil {
        httpx.Write(w, r, httpx.Internal(err))
        return
    }
    // Return full current mod list for the instance after sync
    currentMods, _ := dbpkg.ListMods(db, inst.ID)
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(struct {
        Instance  dbpkg.Instance `json:"instance"`
        Unmatched []string       `json:"unmatched"`
        Mods      []dbpkg.Mod    `json:"mods"`
    }{*inst2, unmatched, currentMods})
}

func dashboardHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats, err := dbpkg.GetDashboardStats(db)
		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))
			return
		}
		resp := struct {
			Tracked      int               `json:"tracked"`
			UpToDate     int               `json:"up_to_date"`
			Outdated     int               `json:"outdated"`
			OutdatedMods []dbpkg.Mod       `json:"outdated_mods"`
			Recent       []dbpkg.ModUpdate `json:"recent_updates"`
			LastSync     int64             `json:"last_sync"`
			LatencyP50   int64             `json:"latency_p50"`
			LatencyP95   int64             `json:"latency_p95"`
		}{
			Tracked:      stats.Tracked,
			UpToDate:     stats.UpToDate,
			Outdated:     stats.Outdated,
			OutdatedMods: stats.OutdatedMods,
			Recent:       stats.RecentUpdates,
			LastSync:     lastSync.Load(),
			LatencyP50:   latencyP50.Load(),
			LatencyP95:   latencyP95.Load(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func serveStatic(static fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}
		data, err := fs.ReadFile(static, strings.TrimPrefix(path, "/"))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				data, err = fs.ReadFile(static, "index.html")
				if err != nil {
					http.NotFound(w, r)
					return
				}
				path = "/index.html"
			} else {
				http.NotFound(w, r)
				return
			}
		}
		if path == "/index.html" {
			if nonce, ok := r.Context().Value(nonceCtxKey{}).(string); ok && nonce != "" {
				// Expose nonce via meta tag for client-side frameworks if needed
				meta := []byte("<meta name=\"csp-nonce\" content=\"" + nonce + "\">")
				data = bytes.Replace(data, []byte("<head>"), []byte("<head>\n    "+string(meta)), 1)
				// Also add nonce attribute to inline style tags to satisfy style-src-elem
				// Replace <style> and <style ...> without nonce
				s := string(data)
				s = strings.ReplaceAll(s, "<style>", "<style nonce=\""+nonce+"\">")
				s = strings.ReplaceAll(s, "<style ", "<style nonce=\""+nonce+"\" ")
				data = []byte(s)
			}
		}
		http.ServeContent(w, r, path, time.Now(), bytes.NewReader(data))
	}
}

// CheckUpdates refreshes available versions for stored mods.
func CheckUpdates(ctx context.Context, db *sql.DB) {
	mods, err := dbpkg.ListAllMods(db)
	if err != nil {
		log.Error().Err(err).Msg("list mods")
		return
	}
	for _, m := range mods {
		slug, err := parseModrinthSlug(m.URL)
		if err != nil {
			continue
		}
		if err := populateAvailableVersion(ctx, &m, slug); err != nil {
			continue
		}
		_, err = db.Exec(`UPDATE mods SET available_version=?, available_channel=?, download_url=? WHERE id=?`, m.AvailableVersion, m.AvailableChannel, m.DownloadURL, m.ID)
		if err != nil {
			log.Error().Err(err).Msg("update version")
		}
	}
	lastSync.Store(time.Now().Unix())
}

type modMetadata struct {
    GameVersions []string   `json:"game_versions"`
    Loaders      []string   `json:"loaders"`
    Channels     []string   `json:"channels"`
    Versions     []uiVersion `json:"versions"`
}

// detectGameVersion attempts to find a version variable and validate its current value.
func detectGameVersion(def *pppkg.ServerDefinition, data *pppkg.ServerData) (key, val string, ok bool) {
    if def == nil || data == nil || len(def.Data) == 0 || len(data.Data) == 0 {
        return "", "", false
    }
    // Regex rules
    reKey := regexp.MustCompile(`(?i)(^|_)(mc|minecraft)?_?version($|_)`)
    reVal := regexp.MustCompile(`^\d+\.\d+(?:\.\d+)?(?:[-+][A-Za-z0-9._-]+)?$`)

    type candidate struct {
        key     string
        score   int
        options int
        val     string
    }
    best := candidate{score: -1}
    for k, meta := range def.Data {
        disp := strings.ToLower(meta.Display)
        desc := strings.ToLower(meta.Desc)
        matchesText := strings.Contains(disp, "version") || strings.Contains(desc, "version")
        matchesKey := reKey.MatchString(strings.ToLower(k))
        if !(matchesText || matchesKey) {
            continue
        }
        // Validate value existence
        vw, okd := data.Data[k]
        if !okd || vw.Value == nil {
            continue
        }
        s := 0
        if matchesKey { s += 2 } else if matchesText { s += 1 }
        // Options heuristic
        optCount := 0
        if len(meta.Options) > 0 {
            for _, o := range meta.Options {
                if reVal.MatchString(strings.TrimSpace(o)) {
                    optCount++
                }
            }
            if optCount > 0 { s += 2 }
        }
        // Extract string value
        var vStr string
        switch x := vw.Value.(type) {
        case string:
            vStr = strings.TrimSpace(x)
        default:
            // try to marshal then convert
            b, _ := json.Marshal(x)
            vStr = strings.Trim(string(b), `"`)
        }
        if !reVal.MatchString(vStr) {
            continue
        }
        // prefer when exact match exists in options
        if optCount > 0 {
            for _, o := range meta.Options {
                if strings.TrimSpace(o) == vStr {
                    s += 1
                    break
                }
            }
        }
        c := candidate{key: k, score: s, options: optCount, val: vStr}
        if c.score > best.score || (c.score == best.score && c.options > best.options) {
            best = c
        }
    }
    if best.score >= 0 {
        return best.key, best.val, true
    }
    return "", "", false
}

// uiVersion mirrors mr.Version JSON while adding UI helper flags.
type uiVersion struct {
    ID            string        `json:"id"`
    VersionNumber string        `json:"version_number"`
    VersionType   string        `json:"version_type"`
    DatePublished time.Time     `json:"date_published"`
    GameVersions  []string      `json:"game_versions"`
    Loaders       []string      `json:"loaders"`
    Files         []mr.VersionFile `json:"files"`
    IsNewest      bool          `json:"is_newest"`
    IsPrerelease  bool          `json:"is_prerelease"`
}

func fetchModMetadata(ctx context.Context, rawURL string) (*modMetadata, error) {
	slug, err := parseModrinthSlug(rawURL)
	if err != nil {
		return nil, err
	}
    versions, err := modClient.Versions(ctx, slug, "", "")
    if err != nil {
        return nil, err
    }
    meta := &modMetadata{}
    // Determine newest by DatePublished
    var newestIdx int = -1
    for i, v := range versions {
        if newestIdx == -1 || v.DatePublished.After(versions[newestIdx].DatePublished) {
            newestIdx = i
        }
    }
    gvSet := map[string]struct{}{}
    ldSet := map[string]struct{}{}
    chSet := map[string]struct{}{}
    for i, v := range versions {
        // Fill list helpers
        for _, gv := range v.GameVersions {
            gvSet[gv] = struct{}{}
        }
        for _, ld := range v.Loaders {
            ldSet[ld] = struct{}{}
        }
        chSet[strings.ToLower(v.VersionType)] = struct{}{}
        // Add UI-annotated version
        meta.Versions = append(meta.Versions, uiVersion{
            ID:            v.ID,
            VersionNumber: v.VersionNumber,
            VersionType:   v.VersionType,
            DatePublished: v.DatePublished,
            GameVersions:  append([]string(nil), v.GameVersions...),
            Loaders:       append([]string(nil), v.Loaders...),
            Files:         append([]mr.VersionFile(nil), v.Files...),
            IsNewest:      i == newestIdx,
            IsPrerelease:  strings.ToLower(v.VersionType) != "release",
        })
    }
	for gv := range gvSet {
		meta.GameVersions = append(meta.GameVersions, gv)
	}
	for ld := range ldSet {
		meta.Loaders = append(meta.Loaders, ld)
	}
	for ch := range chSet {
		meta.Channels = append(meta.Channels, ch)
	}
	sort.Strings(meta.GameVersions)
	sort.Strings(meta.Loaders)
	sort.Strings(meta.Channels)
	return meta, nil
}

func populateProjectInfo(ctx context.Context, m *dbpkg.Mod, slug string) error {
	info, err := modClient.Project(ctx, slug)
	if err != nil {
		return err
	}
	m.Name = info.Title
	m.IconURL = info.IconURL
	return nil
}

func populateVersions(ctx context.Context, m *dbpkg.Mod, slug string) error {
	versions, err := guardedVersions(ctx, slug, m.GameVersion, m.Loader)
	if err != nil {
		return err
	}
	var current mr.Version
	found := false
	for _, v := range versions {
		if strings.EqualFold(v.VersionType, m.Channel) {
			current = v
			found = true
			break
		}
	}
	if !found {
		return errors.New("no version found for channel")
	}
	m.CurrentVersion = current.VersionNumber
	if len(current.Files) > 0 {
		m.DownloadURL = current.Files[0].URL
	}
	if err := populateAvailableVersion(ctx, m, slug); err != nil {
		return err
	}
	return nil
}

func populateAvailableVersion(ctx context.Context, m *dbpkg.Mod, slug string) error {
	versions, err := guardedVersions(ctx, slug, m.GameVersion, m.Loader)
	if err != nil {
		return err
	}
	order := []string{"release", "beta", "alpha"}
	idx := map[string]int{"release": 0, "beta": 1, "alpha": 2}
	start := idx[strings.ToLower(m.Channel)]
	for i := 0; i <= start; i++ {
		ch := order[i]
		for _, v := range versions {
			if strings.EqualFold(v.VersionType, ch) {
				m.AvailableVersion = v.VersionNumber
				m.AvailableChannel = ch
				if len(v.Files) > 0 {
					m.DownloadURL = v.Files[0].URL
				}
				return nil
			}
		}
	}
	m.AvailableVersion = m.CurrentVersion
	m.AvailableChannel = m.Channel
	return nil
}

func parseModrinthSlug(raw string) (string, error) {
	u, err := urlpkg.Parse(raw)
	if err != nil {
		return "", err
	}
	parts := strings.Split(u.Path, "/")
	for i, p := range parts {
		if p == "mod" || p == "plugin" || p == "datapack" || p == "resourcepack" {
			if i+1 < len(parts) {
				return parts[i+1], nil
			}
		}
	}
	return "", errors.New("slug not found")
}

type jarMeta struct {
	Slug      string
	ID        string
	Version   string
	MCVersion string
	Loader    string
	Channel   string
}

func parseJarFilename(name string) jarMeta {
	var meta jarMeta
	name = strings.TrimSuffix(strings.ToLower(name), ".jar")
	rep := strings.NewReplacer("[", "", "]", "", "(", "", ")", "", "{", "", "}", "", "#", "")
	name = rep.Replace(name)
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_' || r == '+'
	})
	if len(parts) == 0 {
		return meta
	}
	semver := regexp.MustCompile(`^v?\d+(?:\.\d+){1,3}[^a-zA-Z]*$`)
	mcver := regexp.MustCompile(`^1\.\d+(?:\.\d+)?$`)
	loaders := map[string]struct{}{"fabric": {}, "forge": {}, "quilt": {}, "neoforge": {}}
	channels := map[string]struct{}{"beta": {}, "alpha": {}, "rc": {}}

	type sv struct {
		idx int
		val string
	}
	semvers := []sv{}
	for i, p := range parts {
		if strings.HasPrefix(p, "mc") {
			v := strings.TrimPrefix(p, "mc")
			if mcver.MatchString(v) && meta.MCVersion == "" {
				meta.MCVersion = v
				continue
			}
		}
		if semver.MatchString(p) {
			semvers = append(semvers, sv{i, strings.TrimPrefix(p, "v")})
			continue
		}
		if _, ok := loaders[p]; ok {
			meta.Loader = p
			continue
		}
		if _, ok := channels[p]; ok {
			meta.Channel = p
			continue
		}
	}
	verIdx := -1
	if len(semvers) > 0 {
		last := semvers[len(semvers)-1]
		verIdx = last.idx
		meta.Version = last.val
		if len(semvers) > 1 {
			prev := semvers[len(semvers)-2]
			if mcver.MatchString(last.val) && !mcver.MatchString(prev.val) {
				meta.Version = prev.val
				verIdx = prev.idx
				meta.MCVersion = last.val
			} else if meta.MCVersion == "" {
				for _, sv := range semvers[:len(semvers)-1] {
					if mcver.MatchString(sv.val) {
						meta.MCVersion = sv.val
						break
					}
				}
			}
		}
	}

	for i, p := range parts {
		if verIdx != -1 && i >= verIdx {
			break
		}
		if _, ok := loaders[p]; ok && i > 0 {
			continue
		}
		if strings.HasPrefix(p, "mc") {
			v := strings.TrimPrefix(p, "mc")
			if mcver.MatchString(v) {
				continue
			}
		}
		if mcver.MatchString(p) {
			continue
		}
		if _, ok := channels[p]; ok && i > 0 {
			continue
		}
		meta.Slug += p + "-"
	}
	meta.Slug = strings.Trim(meta.Slug, "-")
	if meta.Slug != "" {
		parts := strings.Split(meta.Slug, "-")
		if len(parts) > 0 {
			meta.ID = parts[0]
		}
	}
	return meta
}

// normalizeCandidate prepares a filename-derived candidate string for lookup
// - lowercases
// - replaces spaces/underscores with dashes
// - drops brackets and parentheses
func normalizeCandidate(s string) string {
    s = strings.ToLower(strings.TrimSpace(s))
    repl := strings.NewReplacer("[", "", "]", "", "(", "", ")", "")
    s = repl.Replace(s)
    s = strings.ReplaceAll(s, " ", "-")
    s = strings.ReplaceAll(s, "_", "-")
    s = strings.Trim(s, "-")
    return s
}

// normalizeVersion trims and simplifies version strings for matching.
// - lowercase
// - trim leading 'v'
// - drop build metadata (+...)
// - strip MC version suffixes like -1.21.5
// - remove loader suffixes like -fabric, -neoforge, -forge, -quilt, -paper, -spigot, -bukkit
// - collapse -b- tags (e.g., -b123)
func normalizeVersion(s string) string {
    s = strings.ToLower(strings.TrimSpace(s))
    s = strings.TrimPrefix(s, "v")
    if i := strings.Index(s, "+"); i >= 0 {
        s = s[:i]
    }
    // strip mc version suffix like -1.21.5 or _1.20
    reMC := regexp.MustCompile(`[-_](?:1\.\d+(?:\.\d+)?)$`)
    s = reMC.ReplaceAllString(s, "")
    // remove loader suffixes at end
    reLoader := regexp.MustCompile(`[-_](fabric|neoforge|forge|quilt|paper|spigot|bukkit)$`)
    s = reLoader.ReplaceAllString(s, "")
    // collapse -b- build tags
    reB := regexp.MustCompile(`[-_]?b\d+`)
    s = reB.ReplaceAllString(s, "")
    s = strings.Trim(s, "-_")
    return s
}

func basenameFromURL(u string) string {
    if u == "" { return "" }
    if parsed, err := urlpkg.Parse(u); err == nil {
        p := parsed.Path
        if i := strings.LastIndex(p, "/"); i >= 0 && i+1 < len(p) {
            return p[i+1:]
        }
        return p
    }
    return u
}

func tokenizeFilename(name string) map[string]struct{} {
    name = strings.ToLower(name)
    re := regexp.MustCompile(`[^a-z0-9]+`)
    parts := re.Split(name, -1)
    set := make(map[string]struct{}, len(parts))
    for _, p := range parts {
        p = strings.TrimSpace(p)
        if len(p) == 0 { continue }
        set[p] = struct{}{}
    }
    return set
}

func jaccard(a, b map[string]struct{}) float64 {
    if len(a) == 0 || len(b) == 0 { return 0 }
    inter := 0
    union := len(a)
    for t := range b { if _, ok := a[t]; ok { inter++ } else { union++ } }
    if union == 0 { return 0 }
    return float64(inter) / float64(union)
}

