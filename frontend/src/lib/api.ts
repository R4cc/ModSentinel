export interface ModVersion {
  id: string;
  version_number: string;
  version_type: string;
  date_published: string;
  game_versions: string[];
  loaders: string[];
}

export interface Mod {
  id: number;
  name: string;
  icon_url: string;
  url: string;
  game_version: string;
  loader: string;
  channel: string;
  current_version: string;
  available_version: string;
  available_channel: string;
  download_url: string;
}

export interface ModMetadata {
  game_versions: string[];
  versions: ModVersion[];
}

export interface DashboardData {
  tracked: number;
  up_to_date: number;
  outdated: number;
  outdated_mods: Mod[];
  recent_updates: ModUpdate[];
  last_sync: number;
  latency_p50: number;
  latency_p95: number;
}

export interface ModUpdate {
  id: number;
  name: string;
  version: string;
  updated_at: string;
}

export async function getModMetadata(url: string): Promise<ModMetadata> {
  const res = await fetch("/api/mods/metadata", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ url }),
  });
  if (res.status === 401) throw new Error("token required");
  if (!res.ok) throw new Error("Failed to fetch metadata");
  return res.json();
}

export async function getMods(): Promise<Mod[]> {
  const res = await fetch("/api/mods");
  if (!res.ok) throw new Error("Failed to fetch mods");
  return res.json();
}

export interface NewMod {
  url: string;
  game_version: string;
  loader: string;
  channel: string;
}

export async function addMod(payload: NewMod): Promise<Mod[]> {
  const res = await fetch("/api/mods", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (res.status === 401) throw new Error("token required");
  if (!res.ok) throw new Error("Failed to add mod");
  return res.json();
}

export async function refreshMod(id: number, payload: NewMod): Promise<Mod[]> {
  const res = await fetch(`/api/mods/${id}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (res.status === 401) throw new Error("token required");
  if (!res.ok) throw new Error("Failed to update mod");
  return res.json();
}

export async function updateModVersion(id: number): Promise<Mod> {
  const res = await fetch(`/api/mods/${id}/update`, { method: 'POST' });
  if (!res.ok) throw new Error('Failed to update mod');
  return res.json();
}

export async function deleteMod(id: number): Promise<Mod[]> {
  const res = await fetch(`/api/mods/${id}`, { method: "DELETE" });
  if (!res.ok) throw new Error("Failed to delete mod");
  return res.json();
}

export async function getDashboard(): Promise<DashboardData> {
  const res = await fetch('/api/dashboard');
  if (res.status === 401) throw new Error('token required');
  if (res.status === 429) throw new Error('rate limited');
  if (!res.ok) throw new Error('Failed to fetch dashboard');
  return res.json();
}

export async function getToken(): Promise<string> {
  const res = await fetch("/api/token");
  if (!res.ok) throw new Error("Failed to fetch token");
  const data: { token: string } = await res.json();
  return data.token;
}

export async function saveToken(token: string): Promise<void> {
  const res = await fetch("/api/token", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ token }),
  });
  if (!res.ok) throw new Error("Failed to save token");
  window.dispatchEvent(new Event("token-change"));
}

export async function clearToken(): Promise<void> {
  const res = await fetch("/api/token", { method: "DELETE" });
  if (!res.ok) throw new Error("Failed to clear token");
  window.dispatchEvent(new Event("token-change"));
}
