package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	dbpkg "modsentinel/internal/db"
	"modsentinel/internal/httpx"
	pppkg "modsentinel/internal/pufferpanel"
	"modsentinel/internal/telemetry"
)

type instanceReq struct {
    Name                string `json:"name"`
    Loader              string `json:"loader"`
    // Accept both camelCase and snake_case for server id from clients
    ServerID            string `json:"serverId"`
    PufferpanelServerID string `json:"pufferpanel_server_id"`
}

func sanitizeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)

	return strings.TrimSpace(s)

}

// validateInstanceReq performs business validations for instance creation.
func validateInstanceReq(ctx context.Context, req *instanceReq) map[string]string {
    req.Name = sanitizeName(req.Name)

    details := map[string]string{}

    // Normalize server id from either field
    serverIDCamel := strings.TrimSpace(req.ServerID)

    serverIDSnake := strings.TrimSpace(req.PufferpanelServerID)

    serverID := serverIDCamel
    if serverID == "" {
        serverID = serverIDSnake
    }

    // Name required only when no server is provided.
    // Preserve stricter behavior for legacy camelCase clients (tests),
    // and relax when snake_case field is used by the frontend.
    requireName := serverIDSnake == ""
    if requireName {
        if req.Name == "" {
            details["name"] = "required"
        } else if len([]rune(req.Name)) > dbpkg.InstanceNameMaxLen {
            details["name"] = "max"
        }
    } else if req.Name != "" && len([]rune(req.Name)) > dbpkg.InstanceNameMaxLen {
        details["name"] = "max"
    }

    // Validate loader against Modrinth tag cache if provided; allow empty
    if strings.TrimSpace(req.Loader) != "" && !isValidLoader(ctx, req.Loader) {
        details["loader"] = "invalid"
    }

    if len(details) > 0 {
        return details
    }

    // Upstream validation only when a server is provided
    if serverID != "" {
        if _, err := ppGetServer(ctx, serverID); err != nil {
            if errors.Is(err, pppkg.ErrNotFound) {
                details["serverId"] = "not_found"
            } else {
                details["upstream"] = "unreachable"
            }
            return details
        }
        folder := "mods"
        switch strings.ToLower(req.Loader) {
        case "paper", "spigot", "bukkit":
            folder = "plugins"
        }
        if _, err := ppListPath(ctx, serverID, folder); err != nil {
            if errors.Is(err, pppkg.ErrNotFound) {
                details["folder"] = "missing"
            } else {
                details["upstream"] = "unreachable"
            }
            return details
        }
    }
    return details
}

func recordLatency(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		next.ServeHTTP(w, r)

		dur := time.Since(start).Milliseconds()

		latencyMu.Lock()

		latencySamples = append(latencySamples, dur)

		if len(latencySamples) > 100 {
			latencySamples = latencySamples[1:]
		}
		samples := append([]int64(nil), latencySamples...)

		latencyMu.Unlock()

		if len(samples) == 0 {
			return
		}
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })

		latencyP50.Store(samples[len(samples)/2])

		idx := (len(samples) * 95) / 100
		if idx >= len(samples) {
			idx = len(samples) - 1
		}
		latencyP95.Store(samples[idx])

	})

}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := uuid.NewString()

		ctx := pppkg.WithRequestID(r.Context(), id)

		next.ServeHTTP(w, r.WithContext(ctx))

	})

}

func securityHeaders(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Split style policy for elements vs. attributes to avoid blocking
        // library-provided inline style attributes while keeping <style> tags
        // protected by a nonce in production.
        styleElem := "style-src-elem 'self'"
        styleAttr := "style-src-attr 'unsafe-inline'"
        ctx := r.Context()

        if os.Getenv("APP_ENV") == "production" {
            nonceBytes := make([]byte, 16)

            if _, err := rand.Read(nonceBytes); err == nil {
                nonce := base64.StdEncoding.EncodeToString(nonceBytes)

                styleElem += " 'nonce-" + nonce + "'"
                ctx = context.WithValue(ctx, nonceCtxKey{}, nonce)

            }
        } else {
            // In development, allow inline styles fully for convenience
            styleElem += " 'unsafe-inline'"
        }
        connect := "connect-src 'self'"
        if host := pppkg.APIHost(); host != "" {
            connect += " " + host
        }
        csp := strings.Join([]string{
            "default-src 'self'",
            "frame-ancestors 'none'",
            "base-uri 'none'",
            styleElem,
            styleAttr,
            connect,
            "img-src 'self' data: https:",
        }, "; ")

        w.Header().Set("Content-Security-Policy", csp)

        next.ServeHTTP(w, r.WithContext(ctx))

    })

}

func csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			http.SetCookie(w, &http.Cookie{Name: "csrf_token", Value: csrfToken, Path: "/", HttpOnly: false, SameSite: http.SameSiteStrictMode})

			next.ServeHTTP(w, r)

			return
		}
		c, err := r.Cookie("csrf_token")

		token := r.Header.Get("X-CSRF-Token")

		if err != nil || token == "" || c.Value != token || token != csrfToken {
			httpx.Write(w, r, httpx.Forbidden("invalid csrf token"))

			return
		}
		next.ServeHTTP(w, r)

	})

}

func requireAdmin() func(http.Handler) http.Handler {
	adminToken := os.Getenv("ADMIN_TOKEN")

	if adminToken == "" {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")

			if !strings.HasPrefix(h, "Bearer ") || strings.TrimPrefix(h, "Bearer ") != adminToken {
				httpx.Write(w, r, httpx.Forbidden("admin only"))

				return
			}
			next.ServeHTTP(w, r)

		})

	}
}

func requireAuth() func(http.Handler) http.Handler {
	token := os.Getenv("ADMIN_TOKEN")

	if token == "" {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")

			if !strings.HasPrefix(h, "Bearer ") || strings.TrimPrefix(h, "Bearer ") != token {
				httpx.Write(w, r, httpx.Unauthorized("token required"))

				return
			}
			next.ServeHTTP(w, r)

		})

	}
}

func methodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Allow", http.MethodPost)

	w.WriteHeader(http.StatusMethodNotAllowed)

}

func goneHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusGone)

}

// New builds a router with all HTTP handlers.
func New(db *sql.DB, dist fs.FS, svc *secrets.Service) http.Handler {
    r := chi.NewRouter()


	r.Use(securityHeaders)

	r.Use(recordLatency)

	r.Use(telemetry.HTTP)

	r.Use(requestIDMiddleware)


	r.Get("/favicon.ico", serveFavicon(dist))

	r.Get("/api/meta/modrinth/loaders", modrinthLoadersHandler(db))

	r.Get("/api/instances", listInstancesHandler(db))

	r.Get("/api/instances/{id}", getInstanceHandler(db))

    r.With(requireAuth()).Get("/api/instances/{id:\\d+}/logs", listInstanceLogsHandler(db))

	r.Post("/api/instances/validate", validateInstanceHandler())

	r.Post("/api/instances", createInstanceHandler(db))

	r.Put("/api/instances/{id}", updateInstanceHandler(db))

	r.Delete("/api/instances/{id}", deleteInstanceHandler(db))

	r.With(requireAuth()).Post("/api/instances/sync", listServersHandler(db))

	r.With(requireAuth()).Post("/api/instances/{id:\\d+}/sync", syncHandler(db))

	r.With(requireAuth()).Get("/api/instances/{id:\\d+}/sync", methodNotAllowed)

	r.With(requireAuth()).Get("/api/jobs/{id:\\d+}", jobProgressHandler(db))

	r.With(requireAuth()).Get("/api/jobs/{id:\\d+}/events", jobEventsHandler(db))

	r.With(requireAuth()).Post("/api/jobs/{id:\\d+}/retry", retryFailedHandler(db))

	r.With(requireAuth()).Delete("/api/jobs/{id:\\d+}", cancelJobHandler(db))

	if allowResyncAlias {
		// Temporary alias; TODO: remove after 2025-01-01.
		r.With(requireAuth()).Post("/api/instances/{id:\\d+}/resync", syncHandler(db))

		r.With(requireAuth()).Get("/api/instances/{id:\\d+}/resync", methodNotAllowed)

	} else {
		r.With(requireAuth()).Post("/api/instances/{id:\\d+}/resync", goneHandler)

		r.With(requireAuth()).Get("/api/instances/{id:\\d+}/resync", goneHandler)

	}
    r.Get("/api/mods", listModsHandler(db))

    r.Post("/api/mods/metadata", metadataHandler())

    r.Get("/api/mods/search", searchModsHandler())

    r.Post("/api/mods", createModHandler(db))

	r.Get("/api/mods/{id}/check", checkModHandler(db))

	r.Put("/api/mods/{id}", updateModHandler(db))

	r.Delete("/api/mods/{id}", deleteModHandler(db))

	r.Post("/api/mods/{id}/update", enqueueModUpdateHandler(db))


	r.With(requireAdmin()).Post("/api/pufferpanel/test", testPufferHandler())


	r.Group(func(g chi.Router) {
		g.Use(requireAdmin())

		g.Use(csrfMiddleware)

		g.Post("/api/settings/secret/{type}", setSecretHandler())

		g.Delete("/api/settings/secret/{type}", deleteSecretHandler())

		g.Get("/api/settings/secret/{type}/status", secretStatusHandler(svc))

	})

	r.Get("/api/dashboard", dashboardHandler(db))


    // In development, serve static assets from disk so changes appear without rebuilding Go.
    // Set APP_ENV=development and run `npm run build:watch` in frontend.
    if strings.ToLower(os.Getenv("APP_ENV")) != "production" {
        if disk, err := fs.Sub(os.DirFS("."), "frontend/dist"); err == nil {
            r.Get("/*", serveStatic(disk))

        } else {
            static, _ := fs.Sub(dist, "frontend/dist")

            r.Get("/*", serveStatic(static))

        }
    } else {
        static, _ := fs.Sub(dist, "frontend/dist")

        r.Get("/*", serveStatic(static))

    }

    return r
}

func searchModsHandler() http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        q := strings.TrimSpace(r.URL.Query().Get("q"))

        if q == "" {
            httpx.Write(w, r, httpx.BadRequest("missing query"))

            return
        }
        res, err := modClient.Search(r.Context(), q)

        if err != nil {
            writeModrinthError(w, r, err)

            return
        }
        type hit struct {
            Slug        string `json:"slug"`
            Title       string `json:"title"`
            Description string `json:"description"`
            IconURL     string `json:"icon_url"`
        }
        out := make([]hit, 0, len(res.Hits))

        for _, h := range res.Hits {
            out = append(out, hit{Slug: h.Slug, Title: h.Title, Description: h.Description, IconURL: h.IconURL})

        }
        w.Header().Set("Content-Type", "application/json")

        json.NewEncoder(w).Encode(out)

    }
}

func serveFavicon(dist fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(dist, "favicon.ico")

		if err != nil {
			http.NotFound(w, r)

			return
		}
		w.Header().Set("Content-Type", "image/x-icon")

		http.ServeContent(w, r, "favicon.ico", time.Now(), bytes.NewReader(data))

	}
}

func listInstancesHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        instances, err := dbpkg.ListInstances(db)

        if err != nil {
            httpx.Write(w, r, httpx.Internal(err))

            return
        }
        w.Header().Set("Content-Type", "application/json")

        // Avoid stale list after add/sync flows
        w.Header().Set("Cache-Control", "no-store")

        // Project to include camelCase fields for gameVersion and gameVersionKey
        outs := make([]instanceOut, 0, len(instances))

        for _, in := range instances {
            outs = append(outs, projectInstance(in))

        }
        json.NewEncoder(w).Encode(outs)

    }
}

// emitRequiresMetric logs a gauge for instances that still require a loader selection.
func emitRequiresMetric(db *sql.DB) {
    if db == nil { return }
    var n int
    if err := db.QueryRow(`SELECT COUNT(1) FROM instances WHERE IFNULL(requires_loader,0)=1`).Scan(&n); err == nil {
        telemetry.Event("metric", map[string]string{
            "name":  "instances_requires_loader",
            "value": strconv.Itoa(n),
        })

    }
}

// ensureModrinthLoaders makes sure the in-memory loader cache is populated and fresh.
func ensureModrinthLoaders(ctx context.Context) error {
    now := time.Now()

    modrinthLoadersMu.RLock()

    fresh := len(modrinthLoadersCache) > 0 && now.Before(modrinthLoadersExpiry)

    modrinthLoadersMu.RUnlock()

    if fresh {
        return nil
    }
    tags, err := fetchModrinthLoaders(ctx)

    if err != nil {
        return err
    }
    modrinthLoadersMu.Lock()

    modrinthLoadersCache = tags
    modrinthLoadersExpiry = time.Now().Add(modrinthLoadersTTL)

    modrinthLoadersMu.Unlock()

    return nil
}

func isValidLoader(ctx context.Context, id string) bool {
    id = strings.ToLower(strings.TrimSpace(id))

    if id == "" {
        // Empty means not setting/changing; allow at validation time
        return true
    }
    if id == "vanilla" {
        // Explicitly reject vanilla as a loader selection
        return false
    }
    // Ensure cache; then check IDs only against cache contents
    _ = ensureModrinthLoaders(ctx)

    modrinthLoadersMu.RLock()

    defer modrinthLoadersMu.RUnlock()

    for _, t := range modrinthLoadersCache {
        if strings.EqualFold(t.ID, id) {
            return true
        }
    }
    return false
}

// metaLoaderOut is the outbound shape for loader tags returned by our API.
type metaLoaderOut struct {
    ID   string `json:"id"`
    Name string `json:"name"`
    Icon string `json:"icon,omitempty"`
}

// fetchModrinthLoaders fetches loader tags from Modrinth and projects them.
func fetchModrinthLoaders(ctx context.Context) ([]metaLoaderOut, error) {
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.modrinth.com/v2/tag/loader", nil)

    if err != nil { return nil, err }
    resp, err := http.DefaultClient.Do(req)

    if err != nil { return nil, err }
    defer resp.Body.Close()

    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        return nil, fmt.Errorf("modrinth loaders: %s", resp.Status)

    }
    var tags []struct {
        Icon  string   `json:"icon"`
        Name  string   `json:"name"`
        Types []string `json:"supported_project_types"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
        return nil, err
    }
    out := make([]metaLoaderOut, 0, len(tags))

    for _, t := range tags {
        lower := strings.ToLower(strings.TrimSpace(t.Name))

        if lower == "" { continue }
        if lower == "vanilla" { continue }
        out = append(out, metaLoaderOut{ID: lower, Name: t.Name, Icon: t.Icon})

    }
    return out, nil
}

// modrinthLoadersHandler returns cached Modrinth loader tags, fetching on cold start or expiry.
func modrinthLoadersHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        now := time.Now()

        modrinthLoadersMu.RLock()

        cached := modrinthLoadersCache
        exp := modrinthLoadersExpiry
        modrinthLoadersMu.RUnlock()

        if len(cached) > 0 && now.Before(exp) {
            w.Header().Set("Content-Type", "application/json")

            json.NewEncoder(w).Encode(cached)

            return
        }
        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)

        defer cancel()

        tags, err := fetchModrinthLoaders(ctx)

        if err != nil {
            // Fallback to last good cache, even if stale
            modrinthLoadersMu.RLock()

            stale := modrinthLoadersCache
            modrinthLoadersMu.RUnlock()

            if len(stale) > 0 {
                w.Header().Set("Content-Type", "application/json")

                json.NewEncoder(w).Encode(stale)

                return
            }
            httpx.Write(w, r, httpx.BadGateway("modrinth unavailable"))

            return
        }
        modrinthLoadersMu.Lock()

        modrinthLoadersCache = tags
        modrinthLoadersExpiry = time.Now().Add(modrinthLoadersTTL)

        modrinthLoadersMu.Unlock()

        // Persist full loader records into DB
        if db != nil {
            req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.modrinth.com/v2/tag/loader", nil)

            if req2 != nil {
                if resp2, err2 := http.DefaultClient.Do(req2); err2 == nil {
                    defer resp2.Body.Close()

                    if resp2.StatusCode >= 200 && resp2.StatusCode < 300 {
                        var raw []struct {
                            Icon  string   `json:"icon"`
                            Name  string   `json:"name"`
                            Types []string `json:"supported_project_types"`
                        }
                        if json.NewDecoder(resp2.Body).Decode(&raw) == nil {
                            entries := make([]dbpkg.LoaderTag, 0, len(raw))

                            for _, t := range raw {
                                lower := strings.ToLower(strings.TrimSpace(t.Name))

                                if lower == "" { continue }
                                if lower == "vanilla" { continue }
                                entries = append(entries, dbpkg.LoaderTag{ID: lower, Name: t.Name, Icon: t.Icon, Types: t.Types})

                            }
                            _ = dbpkg.UpsertModrinthLoaders(db, entries)

                        }
                    }
                }
            }
        }
        // Telemetry + log: record refresh
        telemetry.Event("metric", map[string]string{
            "name":  "modrinth_loaders_last_fetch_epoch",
            "value": strconv.FormatInt(time.Now().Unix(), 10),
        })

        telemetry.Event("metric", map[string]string{
            "name":  "modrinth_loaders_count",
            "value": strconv.Itoa(len(tags)),
        })

        // Custom telemetry: count after filtering (vanilla excluded)

        telemetry.Event("modrinth_loaders_refresh", map[string]string{
            "count": strconv.Itoa(len(tags)),
        })

        log.Info().
            Str("event", "modrinth_loaders_refresh").
            Int("count", len(tags)).
            Int("ttl_sec", int(modrinthLoadersTTL.Seconds())).
            Msg("telemetry")

        w.Header().Set("Content-Type", "application/json")

        json.NewEncoder(w).Encode(tags)

    }
}

func getInstanceHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")

		id, err := strconv.Atoi(idStr)

		if err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid id"))

			return
		}
		inst, err := dbpkg.GetInstance(db, id)

		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))

			return
		}
        w.Header().Set("Content-Type", "application/json")

        // Avoid stale instance stats during/after sync
        w.Header().Set("Cache-Control", "no-store")

        json.NewEncoder(w).Encode(projectInstance(*inst))

    }
}

// instanceOut augments the DB Instance with camelCase fields for API consumers.
type instanceOut struct {
    dbpkg.Instance
    GameVersion       string `json:"gameVersion,omitempty"`
    GameVersionKey    string `json:"gameVersionKey,omitempty"`
    GameVersionSource string `json:"gameVersionSource,omitempty"`
    // Computed flags to represent loader state without schema changes
    LoaderStatus   string `json:"loader_status,omitempty"`
    LoaderRequired bool   `json:"loader_required,omitempty"`
}

func projectInstance(in dbpkg.Instance) instanceOut {
    out := instanceOut{Instance: in}
    // Mirror values to camelCase fields
    out.GameVersion = strings.TrimSpace(in.GameVersion)

    out.GameVersionKey = strings.TrimSpace(in.PufferVersionKey)

    if out.GameVersion != "" {
        if out.GameVersionKey != "" {
            out.GameVersionSource = "pufferpanel"
        } else {
            out.GameVersionSource = "manual"
        }
    }
    // Compute loader state
    isUnknown := strings.TrimSpace(in.Loader) == "" || in.RequiresLoader
    out.LoaderRequired = isUnknown
    if isUnknown {
        out.LoaderStatus = "unknown"
    } else if strings.TrimSpace(in.PufferpanelServerID) == "" {
        // Heuristic: if not linked to a PufferPanel server, assume user_set
        out.LoaderStatus = "user_set"
    } else {
        out.LoaderStatus = "known"
    }
    return out
}

func validateInstanceHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req instanceReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid json"))

			return
		}
		if details := validateInstanceReq(r.Context(), &req); len(details) > 0 {
			telemetry.Event("instance_validation_failed", map[string]string{"correlation_id": uuid.NewString()})

			httpx.Write(w, r, httpx.BadRequest("validation failed").WithDetails(details))

			return
		}
		w.Header().Set("Content-Type", "application/json")

		json.NewEncoder(w).Encode(map[string]bool{"ok": true})

	}
}

func createInstanceHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var req instanceReq
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            httpx.Write(w, r, httpx.BadRequest("invalid json"))

            return
        }
        if details := validateInstanceReq(r.Context(), &req); len(details) > 0 {
            telemetry.Event("instance_validation_failed", map[string]string{"correlation_id": uuid.NewString()})

            httpx.Write(w, r, httpx.BadRequest("validation failed").WithDetails(details))

            return
        }
        // Normalize incoming fields
        serverIDCamel := strings.TrimSpace(req.ServerID)

        serverIDSnake := strings.TrimSpace(req.PufferpanelServerID)

        serverID := serverIDCamel
        if serverID == "" {
            serverID = serverIDSnake
        }
        name := req.Name
        // Only auto-derive name when snake_case field is provided by the frontend flow
        if name == "" && serverIDSnake != "" {
            if s, err := ppGetServer(r.Context(), serverIDSnake); err == nil && s != nil {
                name = sanitizeName(s.Name)

                rn := []rune(name)

                if len(rn) > dbpkg.InstanceNameMaxLen {
                    name = string(rn[:dbpkg.InstanceNameMaxLen])

                }
            }
        }
        // Fallback if derived name is still empty
        if strings.TrimSpace(name) == "" {
            base := "Server"
            if serverIDSnake != "" {
                base = fmt.Sprintf("Server %s", serverIDSnake)

            }
            rn := []rune(base)

            if len(rn) > dbpkg.InstanceNameMaxLen {
                base = string(rn[:dbpkg.InstanceNameMaxLen])

            }
            name = base
        }
        inst := dbpkg.Instance{ID: 0, Name: name, Loader: strings.ToLower(req.Loader), PufferpanelServerID: serverID}
        tx, err := db.BeginTx(r.Context(), nil)

        if err != nil {
            httpx.Write(w, r, httpx.Internal(err))

            return
        }
        res, err := tx.Exec(`INSERT INTO instances(name, loader, pufferpanel_server_id) VALUES(?,?,?)`, inst.Name, inst.Loader, inst.PufferpanelServerID)

		if err != nil {
			tx.Rollback()

			httpx.Write(w, r, httpx.Internal(err))

			return
		}
		id, _ := res.LastInsertId()

		inst.ID = int(id)

		if err := tx.Commit(); err != nil {
			httpx.Write(w, r, httpx.Internal(err))

			return
		}
		w.Header().Set("Content-Type", "application/json")

		w.Header().Set("Cache-Control", "no-store")

		w.WriteHeader(http.StatusCreated)

        json.NewEncoder(w).Encode(projectInstance(inst))

	}
}

func updateInstanceHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")

		id, err := strconv.Atoi(idStr)

		if err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid id"))

			return
		}
		inst, err := dbpkg.GetInstance(db, id)

		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))

			return
		}
    var req struct {
        Name        *string `json:"name"`
        Loader      *string `json:"loader"`
        // Optional manual override for Minecraft version. When provided,
        // we treat the value as a manual setting and clear any PufferPanel key.
        GameVersion *string `json:"gameVersion"`
    }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid json"))

			return
		}
    if req.Loader != nil && strings.TrimSpace(*req.Loader) != "" && !isValidLoader(r.Context(), *req.Loader) {
        httpx.Write(w, r, httpx.BadRequest("invalid loader"))

        return
    }
		if req.Name != nil {
			n := sanitizeName(*req.Name)

			if n == "" {
				httpx.Write(w, r, httpx.BadRequest("validation failed").WithDetails(map[string]string{"name": "required"}))

				return
			}
			if len([]rune(n)) > dbpkg.InstanceNameMaxLen {
				httpx.Write(w, r, httpx.BadRequest("validation failed").WithDetails(map[string]string{"name": "max"}))

				return
			}
			inst.Name = n
		}
    if req.Loader != nil && strings.TrimSpace(*req.Loader) != "" {
        inst.Loader = strings.ToLower(strings.TrimSpace(*req.Loader))

        inst.RequiresLoader = false
        telemetry.Event("loader_set", map[string]string{
            "source":      "user",
            "loader":      inst.Loader,
            "instance_id": strconv.Itoa(inst.ID),
        })

    }
    if req.GameVersion != nil {
        gv := strings.TrimSpace(*req.GameVersion)

        inst.GameVersion = gv
        // Clear puffer key to mark the value as manual
        inst.PufferVersionKey = ""
    }
		if err := validatePayload(inst); err != nil {
			httpx.Write(w, r, err)

			return
		}
		if err := dbpkg.UpdateInstance(db, inst); err != nil {
			httpx.Write(w, r, httpx.Internal(err))

			return
		}
		w.Header().Set("Content-Type", "application/json")

		w.Header().Set("Cache-Control", "no-store")

        json.NewEncoder(w).Encode(projectInstance(*inst))

	}
}

func deleteInstanceHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")

		id, err := strconv.Atoi(idStr)

		if err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid id"))

			return
		}

		var targetID *int
		if tStr := r.URL.Query().Get("target_instance_id"); tStr != "" {
			t, err := strconv.Atoi(tStr)

			if err != nil {
				httpx.Write(w, r, httpx.BadRequest("invalid target_instance_id"))

				return
			}
			targetID = &t
		}

		if err := dbpkg.DeleteInstance(db, id, targetID); err != nil {
			httpx.Write(w, r, httpx.Internal(err))

			return
		}
		w.Header().Set("Cache-Control", "no-store")

		w.WriteHeader(http.StatusNoContent)

	}
}

func listModsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.URL.Query().Get("instance_id")

		id, err := strconv.Atoi(idStr)

		if err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid instance_id"))

			return
		}
		mods, err := dbpkg.ListMods(db, id)

		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))

			return
		}
		w.Header().Set("Content-Type", "application/json")

		w.Header().Set("Cache-Control", "max-age=60")

		json.NewEncoder(w).Encode(mods)

	}
}

func metadataHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req metadataRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid json"))

			return
		}
		if err := validatePayload(&req); err != nil {
			httpx.Write(w, r, err)

			return
		}
		meta, err := fetchModMetadata(r.Context(), req.URL)

		if err != nil {
			writeModrinthError(w, r, err)

			return
		}
		w.Header().Set("Content-Type", "application/json")

		json.NewEncoder(w).Encode(meta)

	}
}

func createModHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Accept core mod fields plus an optional explicit version id chosen in the wizard
        var req struct {
            dbpkg.Mod
            VersionID string `json:"version_id"`
        }
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            httpx.Write(w, r, httpx.BadRequest("invalid json"))

            return
        }
        m := req.Mod
        if err := validatePayload(&m); err != nil {
            httpx.Write(w, r, err)

            return
        }
        inst, err := dbpkg.GetInstance(db, m.InstanceID)

        if err != nil {
            httpx.Write(w, r, httpx.Internal(err))

            return
        }
        if inst.RequiresLoader {
            telemetry.Event("action_blocked", map[string]string{"action": "add", "reason": "loader_required", "instance_id": strconv.Itoa(inst.ID)})

            httpx.Write(w, r, httpx.LoaderRequired())

            return
        }
        warning := ""
        if !strings.EqualFold(inst.Loader, m.Loader) {
            // No enforcement; surface as a warning for clients that care
            warning = "loader mismatch"
        }
        slug, err := parseModrinthSlug(m.URL)

        if err != nil {
            httpx.Write(w, r, httpx.BadRequest(err.Error()))

            return
        }
        if err := populateProjectInfo(r.Context(), &m, slug); err != nil {
            writeModrinthError(w, r, err)

            return
        }
        // If client provided an explicit Modrinth version ID, honor it.
        // Keep the selected file URL separate so later enrichment does not overwrite it.
        selectedURL := ""
        selectedVersion := ""
        if vid := strings.TrimSpace(req.VersionID); vid != "" {
            versions, err := guardedVersions(r.Context(), slug, m.GameVersion, m.Loader)

            if err != nil {
                writeModrinthError(w, r, err)

                return
            }
            found := false
            for _, v := range versions {
                if v.ID == vid {
                    m.CurrentVersion = v.VersionNumber
                    m.Channel = strings.ToLower(v.VersionType)

                    if len(v.Files) > 0 {
                        m.DownloadURL = v.Files[0].URL
                        selectedURL = m.DownloadURL
                    }
                    selectedVersion = m.CurrentVersion
                    found = true
                    break
                }
            }
            if !found {
                httpx.Write(w, r, httpx.BadRequest("selected version not found"))

                return
            }
            if err := populateAvailableVersion(r.Context(), &m, slug); err != nil {
                writeModrinthError(w, r, err)

                return
            }
        } else {
            if err := populateVersions(r.Context(), &m, slug); err != nil {
                writeModrinthError(w, r, err)

                return
            }
        }
        if err := dbpkg.InsertMod(db, &m); err != nil {
            httpx.Write(w, r, httpx.Internal(err))

            return
        }
        // Log event: mod added (best-effort)

        _ = dbpkg.InsertEvent(db, &dbpkg.ModEvent{InstanceID: m.InstanceID, ModID: &m.ID, Action: "added", ModName: m.Name, To: m.CurrentVersion})

        // If this instance is linked to PufferPanel, attempt to download the selected file
        // and upload it to the appropriate folder on the server (mods/ or plugins/).
        // Use the explicitly selected version file if provided, otherwise fall back to current m.DownloadURL.
        dlURL := m.DownloadURL
        verForName := m.CurrentVersion
        if selectedURL != "" {
            dlURL = selectedURL
            if selectedVersion != "" {
                verForName = selectedVersion
            }
        }
        if inst.PufferpanelServerID != "" && dlURL != "" {
            folder := "mods/"
            switch strings.ToLower(inst.Loader) {
            case "paper", "spigot", "bukkit":
                folder = "plugins/"
            }
            // Derive filename from URL path; fallback to slug-version.jar
            filename := func(raw string) string {
                if u, err := urlpkg.Parse(raw); err == nil {
                    p := u.Path
                    if i := strings.LastIndex(p, "/"); i != -1 && i+1 < len(p) {
                        name := p[i+1:]
                        if name != "" {
                            return name
                        }
                    }
                }
                base := slug
                if base == "" {
                    base = m.Name
                }
                base = strings.TrimSpace(base)

                if base == "" {
                    base = "mod"
                }
                ver := strings.TrimSpace(verForName)

                if ver == "" {
                    ver = "latest"
                }
                return base + "-" + ver + ".jar"
            }(dlURL)

            // Fetch file bytes
            reqDL, err := http.NewRequestWithContext(r.Context(), http.MethodGet, dlURL, nil)

            if err == nil {
                resp, err := http.DefaultClient.Do(reqDL)

                if err == nil {
                    defer resp.Body.Close()

                    if resp.StatusCode >= 200 && resp.StatusCode < 300 {
                        data, _ := io.ReadAll(resp.Body)

                        if len(data) > 0 {
                            if err := pppkg.PutFile(r.Context(), inst.PufferpanelServerID, folder+filename, data); err != nil {
                                // Surface as a non-fatal warning
                                if warning == "" {
                                    warning = "failed to upload file to PufferPanel"
                                }
                            }
                        } else if warning == "" {
                            warning = "failed to download selected file"
                        }
                    } else if warning == "" {
                        warning = "failed to download selected file"
                    }
                } else if warning == "" {
                    warning = "failed to download selected file"
                }
            } else if warning == "" {
                warning = "failed to download selected file"
            }
        }
        mods, err := dbpkg.ListMods(db, m.InstanceID)

        if err != nil {
            httpx.Write(w, r, httpx.Internal(err))

            return
        }
        w.Header().Set("Content-Type", "application/json")

        w.Header().Set("Cache-Control", "no-store")

        json.NewEncoder(w).Encode(struct {
            Mods    []dbpkg.Mod `json:"mods"`
            Warning string      `json:"warning,omitempty"`
        }{mods, warning})

    }
}

func checkModHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")

		id, err := strconv.Atoi(idStr)

		if err != nil {
			httpx.Write(w, r, httpx.BadRequest("invalid id"))

			return
		}
		m, err := dbpkg.GetMod(db, id)

		if err != nil {
			httpx.Write(w, r, httpx.Internal(err))

			return
		}
        if inst, err2 := dbpkg.GetInstance(db, m.InstanceID); err2 == nil {
            if inst.RequiresLoader {
                telemetry.Event("action_blocked", map[string]string{"action": "update", "reason": "loader_required", "instance_id": strconv.Itoa(inst.ID)})

                httpx.Write(w, r, httpx.LoaderRequired())

                return
            }
        }
		slug, err := parseModrinthSlug(m.URL)

		if err != nil {
			httpx.Write(w, r, httpx.BadRequest(err.Error()))

			return
		}
		if err := populateAvailableVersion(r.Context(), m, slug); err != nil {
			writeModrinthError(w, r, err)

			return
		}
		w.Header().Set("Content-Type", "application/json")

		w.Header().Set("Cache-Control", "no-store")

		json.NewEncoder(w).Encode(m)

	}
}

func updateModHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        idStr := chi.URLParam(r, "id")

        id, err := strconv.Atoi(idStr)

        if err != nil {
            httpx.Write(w, r, httpx.BadRequest("invalid id"))

            return
        }
        // Load existing mod to detect version/file changes for PufferPanel
        prev, err := dbpkg.GetMod(db, id)

        if err != nil {
            httpx.Write(w, r, httpx.Internal(err))

            return
        }
        var m dbpkg.Mod
        if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
            httpx.Write(w, r, httpx.BadRequest("invalid json"))

            return
        }
		m.ID = id
		if err := validatePayload(&m); err != nil {
			httpx.Write(w, r, err)

			return
		}
        if inst, err2 := dbpkg.GetInstance(db, m.InstanceID); err2 == nil {
            if inst.RequiresLoader {
                telemetry.Event("action_blocked", map[string]string{"action": "update", "reason": "loader_required", "instance_id": strconv.Itoa(inst.ID)})

                httpx.Write(w, r, httpx.LoaderRequired())

                return
            }
        }
		slug, err := parseModrinthSlug(m.URL)

		if err != nil {
			httpx.Write(w, r, httpx.BadRequest(err.Error()))

			return
		}
		if err := populateProjectInfo(r.Context(), &m, slug); err != nil {
			writeModrinthError(w, r, err)

			return
		}
		if err := populateVersions(r.Context(), &m, slug); err != nil {
			writeModrinthError(w, r, err)

			return
		}
        if err := dbpkg.UpdateMod(db, &m); err != nil {
            httpx.Write(w, r, httpx.Internal(err))

            return
        }
        if prev.CurrentVersion != m.CurrentVersion {
            _ = dbpkg.InsertEvent(db, &dbpkg.ModEvent{InstanceID: m.InstanceID, ModID: &m.ID, Action: "updated", ModName: m.Name, From: prev.CurrentVersion, To: m.CurrentVersion})

        }
        // If instance is linked to PufferPanel and the version changed, reflect update on server
        if inst, err2 := dbpkg.GetInstance(db, m.InstanceID); err2 == nil && inst.PufferpanelServerID != "" {
            folder := "mods/"
            switch strings.ToLower(inst.Loader) {
            case "paper", "spigot", "bukkit":
                folder = "plugins/"
            }
            // Helper to derive filename from URL or fallback slug-version.jar
            deriveName := func(rawURL, slug, defName, version string) string {
                if u, err := urlpkg.Parse(rawURL); err == nil {
                    p := u.Path
                    if i := strings.LastIndex(p, "/"); i != -1 && i+1 < len(p) {
                        name := p[i+1:]
                        if name != "" {
                            return name
                        }
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

            newSlug, _ := parseModrinthSlug(m.URL)

            oldName := deriveName(prev.DownloadURL, oldSlug, prev.Name, prev.CurrentVersion)

            newName := deriveName(m.DownloadURL, newSlug, m.Name, m.CurrentVersion)

            if oldName != newName || prev.CurrentVersion != m.CurrentVersion {
                // Upload new first, verify, then delete old
                uploaded := false
                if m.DownloadURL != "" {
                    if reqDL, err := http.NewRequestWithContext(r.Context(), http.MethodGet, m.DownloadURL, nil); err == nil {
                        if resp, err := http.DefaultClient.Do(reqDL); err == nil {
                            defer resp.Body.Close()

                            if resp.StatusCode >= 200 && resp.StatusCode < 300 {
                                if data, err := io.ReadAll(resp.Body); err == nil && len(data) > 0 {
                                    if err := pppkg.PutFile(r.Context(), inst.PufferpanelServerID, folder+newName, data); err == nil {
                                        if files, err := pppkg.ListPath(r.Context(), inst.PufferpanelServerID, folder); err == nil {
        for _, f := range files {
                                                if !f.IsDir && strings.EqualFold(f.Name, newName) { uploaded = true; break }
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
                if uploaded {
                    if files, err := pppkg.ListPath(r.Context(), inst.PufferpanelServerID, folder); err == nil {
                        for _, f := range files {
                            if !f.IsDir && strings.EqualFold(f.Name, oldName) {
                                _ = pppkg.DeleteFile(r.Context(), inst.PufferpanelServerID, folder+oldName)

                                break
                            }
                        }
                    } else {
                        _ = pppkg.DeleteFile(r.Context(), inst.PufferpanelServerID, folder+oldName)

                    }
                }
            }
        }
        mods, err := dbpkg.ListMods(db, m.InstanceID)

        if err != nil {
            httpx.Write(w, r, httpx.Internal(err))

            return
        }
		w.Header().Set("Content-Type", "application/json")

		w.Header().Set("Cache-Control", "no-store")

		json.NewEncoder(w).Encode(mods)

	}
}

func deleteModHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        idStr := chi.URLParam(r, "id")

        id, err := strconv.Atoi(idStr)

        if err != nil {
            http.Error(w, "invalid id", http.StatusBadRequest)

            return
        }
        instStr := r.URL.Query().Get("instance_id")

        instID, err := strconv.Atoi(instStr)

        if err != nil {
            http.Error(w, "invalid instance_id", http.StatusBadRequest)

            return
        }
        if inst, err := dbpkg.GetInstance(db, instID); err == nil {
            if inst.RequiresLoader {
                httpx.Write(w, r, httpx.LoaderRequired())

                return
            }
        }
        // Attempt to delete the file from PufferPanel if linked
        var before *dbpkg.Mod
        if mb, err := dbpkg.GetMod(db, id); err == nil { before = mb }
        if m, err := dbpkg.GetMod(db, id); err == nil {
            if inst, err2 := dbpkg.GetInstance(db, m.InstanceID); err2 == nil && inst.PufferpanelServerID != "" {
                folder := "mods/"
                switch strings.ToLower(inst.Loader) {
                case "paper", "spigot", "bukkit":
                    folder = "plugins/"
                }
                slug, _ := parseModrinthSlug(m.URL)

                // Candidate names: URL basename then slug-version.jar
                candidates := []string{}
                if u, err := urlpkg.Parse(m.DownloadURL); err == nil {
                    if p := u.Path; p != "" {
                        if i := strings.LastIndex(p, "/"); i != -1 && i+1 < len(p) {
                            if name := p[i+1:]; name != "" { candidates = append(candidates, name) }
                        }
                    }
                }
                base := strings.TrimSpace(slug)

                if base == "" { base = strings.TrimSpace(m.Name) }
                if base == "" { base = "mod" }
                ver := strings.TrimSpace(m.CurrentVersion)

                if ver == "" { ver = "latest" }
                candidates = append(candidates, base+"-"+ver+".jar")

                if files, err := pppkg.ListPath(r.Context(), inst.PufferpanelServerID, folder); err == nil {
                    present := map[string]bool{}
                    for _, f := range files { present[strings.ToLower(f.Name)] = true }
                    for _, nm := range candidates {
                        if present[strings.ToLower(nm)] { _ = pppkg.DeleteFile(r.Context(), inst.PufferpanelServerID, folder+nm); break }
                    }
                } else {
                    for _, nm := range candidates { _ = pppkg.DeleteFile(r.Context(), inst.PufferpanelServerID, folder+nm) }
                }
            }
        }
        if err := dbpkg.DeleteMod(db, id); err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)

            return
        }
        if before != nil {
            _ = dbpkg.InsertEvent(db, &dbpkg.ModEvent{InstanceID: before.InstanceID, ModID: &before.ID, Action: "deleted", ModName: before.Name, From: before.CurrentVersion})

        }
        mods, err := dbpkg.ListMods(db, instID)

        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)

            return
        }
		w.Header().Set("Content-Type", "application/json")

		w.Header().Set("Cache-Control", "no-store")

		json.NewEncoder(w).Encode(mods)

	}
}

func applyUpdateHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        idStr := chi.URLParam(r, "id")

        id, err := strconv.Atoi(idStr)

        if err != nil {
            httpx.Write(w, r, httpx.BadRequest("invalid id"))

            return
        }
        // Load existing for old version/filename
        prev, err := dbpkg.GetMod(db, id)

        if err != nil {
            httpx.Write(w, r, httpx.Internal(err))

            return
        }
        // Determine target version (the available one) and its file URL from Modrinth
        slug, err := parseModrinthSlug(prev.URL)

        if err != nil {
            httpx.Write(w, r, httpx.BadRequest("invalid mod URL"))

            return
        }
        if strings.TrimSpace(prev.AvailableVersion) == "" || prev.AvailableVersion == prev.CurrentVersion {
            httpx.Write(w, r, httpx.BadRequest("no update available"))

            return
        }
        // Fetch all versions for the project; avoid over-filtering so we can match exact version_number
        versions, err := modClient.Versions(r.Context(), slug, "", "")

        if err != nil {
            writeModrinthError(w, r, err)

            return
        }
        var newVer mr.Version
        found := false
        for _, vv := range versions {
            if vv.VersionNumber == prev.AvailableVersion {
                newVer = vv
                found = true
                break
            }
        }
        if !found {
            httpx.Write(w, r, httpx.BadRequest("selected update not found"))

            return
        }
        if len(newVer.Files) == 0 || strings.TrimSpace(newVer.Files[0].URL) == "" {
            httpx.Write(w, r, httpx.BadRequest("no downloadable file for update"))

            return
        }
        targetURL := newVer.Files[0].URL

        // Mirror change to PufferPanel if configured: upload new first, verify, then delete old
        if inst, err2 := dbpkg.GetInstance(db, prev.InstanceID); err2 == nil && inst.PufferpanelServerID != "" {
            folder := "mods/"
            switch strings.ToLower(inst.Loader) {
            case "paper", "spigot", "bukkit":
                folder = "plugins/"
            }
            deriveName := func(rawURL, slug, defName, version string) string {
                if u, err := urlpkg.Parse(rawURL); err == nil {
                    p := u.Path
                    if i := strings.LastIndex(p, "/"); i != -1 && i+1 < len(p) {
                        name := p[i+1:]
                        if name != "" { return name }
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


            // Download new artifact
            reqDL, err := http.NewRequestWithContext(r.Context(), http.MethodGet, targetURL, nil)

            if err != nil {
                httpx.Write(w, r, httpx.Internal(err))

                return
            }
            resp, err := http.DefaultClient.Do(reqDL)

            if err != nil {
                httpx.Write(w, r, httpx.Internal(err))

                return
            }
            defer resp.Body.Close()

            if resp.StatusCode < 200 || resp.StatusCode >= 300 {
                httpx.Write(w, r, httpx.BadRequest("failed to download update file"))

                return
            }
            // Prevent excessive memory usage for unexpectedly large artifacts
            const maxArtifactSize = 128 << 20 // 128 MiB
            if resp.ContentLength > maxArtifactSize {
                httpx.Write(w, r, httpx.BadRequest("update file too large"))

                return
            }
            data, err := io.ReadAll(io.LimitReader(resp.Body, maxArtifactSize+1))

            if err != nil || len(data) == 0 {
                httpx.Write(w, r, httpx.Internal(fmt.Errorf("invalid file content")))

                return
            }
            if len(data) > maxArtifactSize {
                httpx.Write(w, r, httpx.BadRequest("update file too large"))

                return
            }
            if err := pppkg.PutFile(r.Context(), inst.PufferpanelServerID, folder+newName, data); err != nil {
                writePPError(w, r, err)

                return
            }
            // Verify presence
            if files, err := pppkg.ListPath(r.Context(), inst.PufferpanelServerID, folder); err == nil {
                present := false
                for _, f := range files {
                    if !f.IsDir && strings.EqualFold(f.Name, newName) { present = true; break }
                }
                if !present {
                    httpx.Write(w, r, httpx.Internal(fmt.Errorf("update verification failed")))

                    return
                }
            } else {
                writePPError(w, r, err)

                return
            }
            // Delete old (best-effort)

            if files, err := pppkg.ListPath(r.Context(), inst.PufferpanelServerID, folder); err == nil {
                for _, f := range files {
                    if !f.IsDir && strings.EqualFold(f.Name, oldName) { _ = pppkg.DeleteFile(r.Context(), inst.PufferpanelServerID, folder+oldName); break }
                }
            } else { _ = pppkg.DeleteFile(r.Context(), inst.PufferpanelServerID, folder+oldName) }
        }
        // Now commit DB update to reflect PufferPanel (only after upload verified)

        if _, err := db.Exec(`UPDATE mods SET current_version=?, channel=?, download_url=? WHERE id=?`, prev.AvailableVersion, prev.AvailableChannel, targetURL, prev.ID); err != nil {
            httpx.Write(w, r, httpx.Internal(err))

            return
        }
        // Record update in updates table and fetch updated row
        _ = dbpkg.InsertUpdateIfNew(db, prev.ID, prev.AvailableVersion)

        m, err := dbpkg.GetMod(db, prev.ID)

        if err != nil {
            httpx.Write(w, r, httpx.Internal(err))

            return
        }
        _ = dbpkg.InsertEvent(db, &dbpkg.ModEvent{InstanceID: m.InstanceID, ModID: &m.ID, Action: "updated", ModName: m.Name, From: prev.CurrentVersion, To: m.CurrentVersion})

        w.Header().Set("Content-Type", "application/json")

        json.NewEncoder(w).Encode(m)

    }
}

// enqueueModUpdateHandler enqueues an async update job for a mod and returns { job_id }.
func enqueueModUpdateHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        idStr := chi.URLParam(r, "id")

        id, err := strconv.Atoi(idStr)

        if err != nil {
            httpx.Write(w, r, httpx.BadRequest("invalid id"))

            return
        }
        var payload struct{
            IdempotencyKey string `json:"idempotency_key"`
        }
        if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
            httpx.Write(w, r, httpx.BadRequest("invalid json"))

            return
        }
        if strings.TrimSpace(payload.IdempotencyKey) == "" {
            httpx.Write(w, r, httpx.BadRequest("validation failed").WithDetails(map[string]string{"idempotency_key": "required"}))

            return
        }
        // Ensure instance does not require loader before enqueuing
        if mu, err0 := dbpkg.GetMod(db, id); err0 == nil {
            if inst, err1 := dbpkg.GetInstance(db, mu.InstanceID); err1 == nil && inst.RequiresLoader {
                telemetry.Event("action_blocked", map[string]string{"action": "update", "reason": "loader_required", "instance_id": strconv.Itoa(inst.ID)})

                httpx.Write(w, r, httpx.LoaderRequired())

                return
            }
        }
        jobID, err := enqueueUpdateJobWithKey(r.Context(), db, id, payload.IdempotencyKey)

        if err != nil {
            httpx.Write(w, r, httpx.Internal(err))

            return
        }
        w.Header().Set("Content-Type", "application/json")

        json.NewEncoder(w).Encode(struct{ JobID int `json:"job_id"` }{jobID})

    }
}

func listInstanceLogsHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        idStr := chi.URLParam(r, "id")

        id, err := strconv.Atoi(idStr)

        if err != nil {
            httpx.Write(w, r, httpx.BadRequest("invalid id"))

            return
        }
        limit := 100
        if s := strings.TrimSpace(r.URL.Query().Get("limit")); s != "" {
            if n, err := strconv.Atoi(s); err == nil {
                limit = n
            }
        }
        events, err := dbpkg.ListEvents(db, id, limit)

        if err != nil {
            httpx.Write(w, r, httpx.Internal(err))

            return
        }
        w.Header().Set("Content-Type", "application/json")

        json.NewEncoder(w).Encode(events)

    }
}




