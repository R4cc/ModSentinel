package summary

import (
    "testing"
    dbpkg "modsentinel/internal/db"
)

func TestSummarizeCounts(t *testing.T) {
    mods := []dbpkg.Mod{
        { // up to date (equal)
            CurrentVersion:   "1.0.0",
            AvailableVersion: "1.0.0",
        },
        { // up to date (no available)
            CurrentVersion:   "2.0.0",
            AvailableVersion: "",
        },
        { // update available
            CurrentVersion:   "3.0.0",
            AvailableVersion: "3.1.0",
        },
        { // update available
            CurrentVersion:   "4.0.0",
            AvailableVersion: "5.0.0",
        },
    }
    unresolved := []string{"unmatched-a.jar", "unmatched-b.jar"}

    got := Summarize(mods, unresolved)

    if got.ModsUpToDate != 2 {
        t.Fatalf("ModsUpToDate = %d, want %d", got.ModsUpToDate, 2)
    }
    if got.ModsUpdateAvailable != 2 {
        t.Fatalf("ModsUpdateAvailable = %d, want %d", got.ModsUpdateAvailable, 2)
    }
    if got.ModsFailed != len(unresolved) {
        t.Fatalf("ModsFailed = %d, want %d", got.ModsFailed, len(unresolved))
    }
}

