package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

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












