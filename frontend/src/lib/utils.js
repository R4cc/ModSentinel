import { clsx } from 'clsx';
import { twMerge } from 'tailwind-merge';

export function cn(...inputs) {
  return twMerge(clsx(inputs));
}

// Summarize an instance's mods into simple counts.
// Pass the list of mods for an instance. Include any "virtual" entries
// the UI creates for unmatched files if you want those counted as failures.
// Returns an object like:
// { mods_up_to_date: number, mods_update_available: number, mods_failed: number }
export function summarizeMods(mods) {
  let mods_up_to_date = 0;
  let mods_update_available = 0;
  let mods_failed = 0;

  for (const m of mods || []) {
    if (m && m.virtual) {
      mods_failed += 1;
      continue;
    }
    const cur = (m && m.current_version) || "";
    const avail = (m && m.available_version) || "";
    // Treat as update-available only when we know a different available version
    if (avail !== "" && avail !== cur) mods_update_available += 1;
    else mods_up_to_date += 1;
  }

  return { mods_up_to_date, mods_update_available, mods_failed };
}
