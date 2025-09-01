package summary

import dbpkg "modsentinel/internal/db"

// Summary represents aggregated mod status counts for an instance.
type Summary struct {
    ModsUpToDate       int `json:"mods_up_to_date"`
    ModsUpdateAvailable int `json:"mods_update_available"`
    ModsFailed          int `json:"mods_failed"`
}

// Summarize computes counts from a list of mods and a list of unresolved file names.
// A mod is considered up-to-date when available version is empty or equals current.
// A mod is considered update-available when available version is set and differs from current.
// ModsFailed counts unresolved entries (e.g., files that failed to match/add).
func Summarize(mods []dbpkg.Mod, unresolved []string) Summary {
    var s Summary
    for _, m := range mods {
        cur := m.CurrentVersion
        avail := m.AvailableVersion
        if avail != "" && avail != cur {
            s.ModsUpdateAvailable++
        } else {
            s.ModsUpToDate++
        }
    }
    s.ModsFailed = len(unresolved)
    return s
}

