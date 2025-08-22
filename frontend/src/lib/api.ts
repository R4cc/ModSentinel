export interface ModVersion {
  id: string;
  version_number: string;
  version_type: string;
  date_published: string;
  game_versions: string[];
  loaders: string[];
}

export interface Instance {
  id: number;
  name: string;
  loader: string;
  pufferpanel_server_id?: string;
  enforce_same_loader: boolean;
  created_at: string;
  mod_count: number;
  last_sync_at?: string;
  last_sync_added?: number;
  last_sync_updated?: number;
  last_sync_failed?: number;
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
  instance_id: number;
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

async function parseError(res: Response, fallback: string): Promise<Error> {
  try {
    const err = await res.json();
    if (err?.code && err?.message) {
      return new Error(`${err.code}: ${err.message}`);
    }
    if (err?.message) return new Error(err.message);
  } catch {
    // ignore
  }
  return new Error(fallback);
}

export async function getModMetadata(url: string): Promise<ModMetadata> {
  const res = await fetch("/api/mods/metadata", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ url }),
  });
  if (res.status === 401) throw new Error("token required");
  if (!res.ok) throw await parseError(res, "Failed to fetch metadata");
  return res.json();
}

export async function getMods(instanceId: number): Promise<Mod[]> {
  const res = await fetch(`/api/mods?instance_id=${instanceId}`, {
    cache: "no-store",
  });
  if (!res.ok) throw await parseError(res, "Failed to fetch mods");
  return res.json();
}

export async function getInstances(): Promise<Instance[]> {
  const res = await fetch("/api/instances", { cache: "no-store" });
  if (!res.ok) throw await parseError(res, "Failed to fetch instances");
  return res.json();
}

export async function getInstance(id: number): Promise<Instance> {
  const res = await fetch(`/api/instances/${id}`, { cache: "no-store" });
  if (!res.ok) throw await parseError(res, "Failed to fetch instance");
  return res.json();
}

export async function deleteInstance(
  id: number,
  targetInstanceId?: number,
): Promise<void> {
  const url = targetInstanceId
    ? `/api/instances/${id}?target_instance_id=${targetInstanceId}`
    : `/api/instances/${id}`;
  const res = await fetch(url, { method: "DELETE" });
  if (!res.ok) throw await parseError(res, "Failed to delete instance");
}

export interface NewMod {
  url: string;
  game_version: string;
  loader: string;
  channel: string;
  instance_id: number;
}

export interface NewInstance {
  name: string;
  loader: string;
  enforce_same_loader: boolean;
  pufferpanel_server_id?: string;
}

export interface UpdateInstance {
  name?: string;
  enforce_same_loader?: boolean;
}

export interface AddModResponse {
  mods: Mod[];
  warning?: string;
}

export async function addMod(payload: NewMod): Promise<AddModResponse> {
  const res = await fetch("/api/mods", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (res.status === 401) throw new Error("token required");
  if (!res.ok) throw await parseError(res, "Failed to add mod");
  return res.json();
}

export async function addInstance(payload: NewInstance): Promise<Instance> {
  const res = await fetch("/api/instances", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (!res.ok) throw await parseError(res, "Failed to add instance");
  return res.json();
}

export async function updateInstance(
  id: number,
  payload: UpdateInstance,
): Promise<Instance> {
  const res = await fetch(`/api/instances/${id}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (!res.ok) throw await parseError(res, "Failed to update instance");
  return res.json();
}

export async function refreshMod(id: number, payload: NewMod): Promise<Mod[]> {
  const res = await fetch(`/api/mods/${id}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (res.status === 401) throw new Error("token required");
  if (!res.ok) throw await parseError(res, "Failed to update mod");
  return res.json();
}

export async function checkMod(id: number): Promise<Mod> {
  const res = await fetch(`/api/mods/${id}/check`, { cache: "no-store" });
  if (res.status === 401) throw new Error("token required");
  if (!res.ok) throw await parseError(res, "Failed to check mod");
  return res.json();
}

export async function updateModVersion(id: number): Promise<Mod> {
  const res = await fetch(`/api/mods/${id}/update`, { method: "POST" });
  if (!res.ok) throw await parseError(res, "Failed to update mod");
  return res.json();
}

export async function deleteMod(
  id: number,
  instanceId: number,
): Promise<Mod[]> {
  const res = await fetch(`/api/mods/${id}?instance_id=${instanceId}`, {
    method: "DELETE",
    cache: "no-store",
  });
  if (!res.ok) throw await parseError(res, "Failed to delete mod");
  return res.json();
}

export async function getDashboard(): Promise<DashboardData> {
  const res = await fetch("/api/dashboard");
  if (res.status === 401) throw new Error("token required");
  if (res.status === 429) throw new Error("rate limited");
  if (!res.ok) throw await parseError(res, "Failed to fetch dashboard");
  return res.json();
}

export interface PufferCreds {
  base_url: string;
  client_id: string;
  client_secret: string;
  deep_scan?: boolean;
}

export interface PufferServer {
  id: string;
  name: string;
}

export interface SyncResult {
  instance: Instance;
  unmatched: string[];
  mods: Mod[];
}

export async function resyncInstance(id: number): Promise<SyncResult> {
  const res = await fetch(`/api/instances/${id}/resync`, { method: "POST" });
  if (!res.ok) throw await parseError(res, "Failed to resync");
  return res.json();
}

export async function getPufferCreds(): Promise<PufferCreds> {
  const res = await fetch("/api/pufferpanel");
  if (!res.ok) throw await parseError(res, "Failed to fetch credentials");
  return res.json();
}

export async function testPufferCreds(creds: PufferCreds): Promise<void> {
  const res = await fetch("/api/pufferpanel/test", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(creds),
  });
  if (!res.ok) throw await parseError(res, "Failed to connect");
}

export async function getPufferServers(): Promise<PufferServer[]> {
  const res = await fetch("/api/pufferpanel/servers");
  if (!res.ok) throw await parseError(res, "Failed to fetch servers");
  return res.json();
}

export async function syncInstances(server?: string): Promise<SyncResult> {
  const url = server
    ? `/api/pufferpanel/sync?server=${encodeURIComponent(server)}`
    : "/api/pufferpanel/sync";
  const res = await fetch(url, { method: "POST" });
  if (!res.ok) throw await parseError(res, "Failed to sync");
  return res.json();
}

export interface SecretStatus {
  exists: boolean;
  last4: string;
  updated_at: string;
}

export async function getSecretStatus(type: string): Promise<SecretStatus> {
  const res = await fetch(`/api/settings/secret/${type}/status`, {
    cache: "no-store",
    headers: { Authorization: "Bearer admintok" },
    credentials: "same-origin",
  });
  if (!res.ok) throw await parseError(res, "Failed to fetch status");
  return res.json();
}

export async function saveSecret(type: string, payload: any): Promise<void> {
  const res = await fetch(`/api/settings/secret/${type}`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: "Bearer admintok",
      "X-CSRF-Token": (document.cookie.match(/csrf_token=([^;]+)/)?.[1] ?? ""),
    },
    credentials: "same-origin",
    body: JSON.stringify(payload),
  });
  if (!res.ok) throw await parseError(res, "Failed to save secret");
  window.dispatchEvent(new Event(`${type}-change`));
}

export async function clearSecret(type: string): Promise<void> {
  const res = await fetch(`/api/settings/secret/${type}`, {
    method: "DELETE",
    headers: {
      Authorization: "Bearer admintok",
      "X-CSRF-Token": (document.cookie.match(/csrf_token=([^;]+)/)?.[1] ?? ""),
    },
    credentials: "same-origin",
  });
  if (!res.ok) throw await parseError(res, "Failed to clear secret");
  window.dispatchEvent(new Event(`${type}-change`));
}
