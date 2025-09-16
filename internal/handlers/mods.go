package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	urlpkg "net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	dbpkg "modsentinel/internal/db"
	"modsentinel/internal/httpx"
	mr "modsentinel/internal/modrinth"
	pppkg "modsentinel/internal/pufferpanel"
	"modsentinel/internal/telemetry"
)

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

// mapLoader canonicalizes loader tokens to supported values.
// Supported: fabric, quilt, forge, neoforge, datapack, resourcepack. Never "minecraft".
func mapLoader(s string) string {
    s = strings.ToLower(strings.TrimSpace(s))
    switch s {
    case "fabric":
        return "fabric"
    case "quilt":
        return "quilt"
    case "forge":
        return "forge"
    case "neoforge":
        return "neoforge"
    case "datapack":
        return "datapack"
    case "resourcepack":
        return "resourcepack"
    default:
        // Discard "minecraft" or unknowns
        return ""
    }
}

func parseJarMetadata(data []byte) (slug, version, loader string) {
    zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
    if err != nil {
        return "", "", ""
    }
    for _, f := range zr.File {
        // Fabric/Quilt
        if f.Name == "fabric.mod.json" || f.Name == "quilt.mod.json" {
            rc, err := f.Open()
            if err != nil {
                continue
            }
            var meta struct {
                ID      string `json:"id"`
                Version string `json:"version"`
            }
            if err := json.NewDecoder(rc).Decode(&meta); err == nil {
                slug = meta.ID
                version = meta.Version
                if f.Name == "fabric.mod.json" {
                    loader = "fabric"
                } else {
                    loader = "quilt"
                }
            }
            rc.Close()
            return slug, version, loader
        }
        // Forge / NeoForge
        if strings.EqualFold(f.Name, "META-INF/mods.toml") || strings.EqualFold(f.Name, "META-INF/neoforge.mods.toml") {
            rc, err := f.Open()
            if err != nil {
                continue
            }
            b, _ := io.ReadAll(rc)
            rc.Close()
            s := string(b)
            // very light parsing without a TOML dependency
            // look for first modId and version assignments
            reID := regexp.MustCompile(`(?m)^\s*modId\s*=\s*"([^"]+)"`)
            reVer := regexp.MustCompile(`(?m)^\s*version\s*=\s*"([^"]+)"`)
            if m := reID.FindStringSubmatch(s); len(m) == 2 {
                slug = m[1]
            }
            if m := reVer.FindStringSubmatch(s); len(m) == 2 {
                version = m[1]
            }
            if strings.Contains(strings.ToLower(f.Name), "neoforge") {
                loader = "neoforge"
            } else {
                loader = "forge"
            }
            if slug != "" || version != "" {
                return slug, version, loader
            }
        }
        // Resource packs
        if strings.EqualFold(f.Name, "pack.mcmeta") {
            // We cannot extract id/version reliably, but can mark loader
            loader = "resourcepack"
        }
    }
    return slug, version, loader
}
