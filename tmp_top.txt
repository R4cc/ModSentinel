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
  // Optional enriched fields from backend projection
  gameVersion?: string;
  gameVersionKey?: string;
  gameVersionSource?: "pufferpanel" | "manual";
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

export interface ModEvent {
  id: number;
  instance_id: number;
  mod_id?: number;
  action: "added" | "deleted" | "updated";
  mod_name: string;
  from_version?: string;
  to_version?: string;
  created_at: string;
}

async function parseJSON(res: Response): Promise<any> {
  const ct = res.headers.get("Content-Type") || "";
  if (!ct.includes("application/json")) return undefined;
  const text = await res.text();
  if (!text) return undefined;
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}

class APIError extends Error {
  requestId?: string;
  constructor(message: string, requestId?: string) {
    super(message);
    this.requestId = requestId;
  }
}

const API_ORIGIN = window.location.origin;

function apiFetch(path: string, init?: RequestInit): Promise<Response> {
  return fetch(`${API_ORIGIN}${path}`, init);
}

async function parseError(res: Response): Promise<APIError> {
  if (res.status === 429) {
    return new APIError("rate limited");
  }
  const text = await res.text();
  try {
    const err = JSON.parse(text);
    if (err?.message) {
      return new APIError(err.message, err.requestId);
    }
  } catch {
    // ignore JSON parse errors
  }
  const msg = text || res.statusText;
  return new APIError(`${res.status} ${msg}`.trim());
}

export async function getModMetadata(url: string): Promise<ModMetadata> {
  const res = await apiFetch("/api/mods/metadata", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ url }),
  });
  if (res.status === 401) throw new Error("token required");
  if (!res.ok) throw await parseError(res);
  return parseJSON(res);
}

export interface ModSearchHit {
  slug: string;
  title: string;
  description?: string;
  icon_url?: string;
}

export async function searchMods(query: string): Promise<ModSearchHit[]> {
  const res = await apiFetch(`/api/mods/search?q=${encodeURIComponent(query)}`);
  if (res.status === 401) throw new Error("token required");
  if (!res.ok) throw await parseError(res);
  return parseJSON(res);
}

export async function getMods(instanceId: number): Promise<Mod[]> {
  const res = await apiFetch(`/api/mods?instance_id=${instanceId}`, {
    cache: "no-store",
  });
  if (!res.ok) throw await parseError(res);
  return parseJSON(res);
}

export async function getInstances(): Promise<Instance[]> {
  const res = await apiFetch("/api/instances", { cache: "no-store" });
  if (!res.ok) throw await parseError(res);
  return parseJSON(res);
}

export async function getInstance(id: number): Promise<Instance> {
  const res = await apiFetch(`/api/instances/${id}`, { cache: "no-store" });
  if (!res.ok) throw await parseError(res);
  return parseJSON(res);
}

export async function deleteInstance(
  id: number,
  targetInstanceId?: number,
): Promise<void> {
  const url = targetInstanceId
    ? `/api/instances/${id}?target_instance_id=${targetInstanceId}`
    : `/api/instances/${id}`;
  const res = await apiFetch(url, { method: "DELETE" });
  if (!res.ok) throw await parseError(res);
}

export interface NewMod {
  url: string;
  game_version: string;
  loader: string;
  channel: string;
  // Optional: exact Modrinth version id chosen in wizard
  version_id?: string;
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
  // Optional manual override for Minecraft version
  gameVersion?: string;
}

export interface AddModResponse {
  mods: Mod[];
  warning?: string;
}

export async function addMod(payload: NewMod): Promise<AddModResponse> {
  const res = await apiFetch("/api/mods", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (res.status === 401) throw new Error("token required");
  if (!res.ok) throw await parseError(res);
  return parseJSON(res);
}

export async function addInstance(payload: NewInstance): Promise<Instance> {
  const res = await apiFetch("/api/instances", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (!res.ok) throw await parseError(res);
  return parseJSON(res);
}

export async function updateInstance(
  id: number,
  payload: UpdateInstance,
): Promise<Instance> {
  const res = await apiFetch(`/api/instances/${id}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (!res.ok) throw await parseError(res);
  return parseJSON(res);
}

export async function refreshMod(id: number, payload: NewMod): Promise<Mod[]> {
  const res = await apiFetch(`/api/mods/${id}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (res.status === 401) throw new Error("token required");
  if (!res.ok) throw await parseError(res);
  return parseJSON(res);
}

export async function checkMod(id: number): Promise<Mod> {
  const res = await apiFetch(`/api/mods/${id}/check`, { cache: "no-store" });
  if (res.status === 401) throw new Error("token required");
  if (!res.ok) throw await parseError(res);
  return parseJSON(res);
}

export interface UpdateJobAck { job_id: number }

export async function startModUpdate(
  id: number,
  idempotencyKey: string,
): Promise<UpdateJobAck> {
  const res = await apiFetch(`/api/mods/${id}/update`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ idempotency_key: idempotencyKey }),
  });
  if (!res.ok) throw await parseError(res);
  return parseJSON(res);
}

export async function deleteMod(
  id: number,
  instanceId: number,
): Promise<Mod[]> {
  const res = await apiFetch(`/api/mods/${id}?instance_id=${instanceId}`, {
    method: "DELETE",
    cache: "no-store",
  });
  if (!res.ok) throw await parseError(res);
  return parseJSON(res);
}

export async function getInstanceLogs(id: number, limit = 200): Promise<ModEvent[]> {
  const res = await apiFetch(`/api/instances/${id}/logs?limit=${limit}`, {
    cache: "no-store",
  });
  if (!res.ok) throw await parseError(res);
  return parseJSON(res);
}

export async function getDashboard(): Promise<DashboardData> {
  const res = await apiFetch("/api/dashboard");
  if (res.status === 401) throw new Error("token required");
  if (!res.ok) throw await parseError(res);
  return parseJSON(res);
}

export interface PufferServer {
  id: string;
  name: string;
}

// Historical: SyncResult was returned when sync ran inline.
// The backend now enqueues a job and returns a Job. Clients should
// track progress via /api/jobs/{id} and /events.

export interface Job {
  id: number;
  status: string;
}

export interface JobProgress extends Job {
  total: number;
  processed: number;
  succeeded: number;
  failed: number;
  in_queue: number;
  failures: { name: string; error: string }[];
}

async function syncInstance(id: number): Promise<Job> {
  const res = await apiFetch(`/api/instances/${id}/sync`, { method: "POST" });
  if (!res.ok) throw await parseError(res);
  return parseJSON(res);
}

async function retryJob(id: number): Promise<Job> {
  const res = await apiFetch(`/api/jobs/${id}/retry`, { method: "POST" });
  if (!res.ok) throw await parseError(res);
  return parseJSON(res);
}

export async function getJob(id: number): Promise<JobProgress> {
  const res = await apiFetch(`/api/jobs/${id}`);
  if (!res.ok) throw await parseError(res);
  return parseJSON(res);
}

export const instances = {
  sync: syncInstance,
};

export const jobs = {
  retry: retryJob,
};

export async function getPufferServers(): Promise<PufferServer[]> {
  const res = await apiFetch("/api/instances/sync", {
    method: "POST",
    headers: { Authorization: "Bearer admintok" },
    credentials: "same-origin",
  });
  if (!res.ok) throw await parseError(res);
  return parseJSON(res);
}

export async function syncInstances(
  serverId: string,
  instanceId: number,
): Promise<Job> {
  const res = await apiFetch(`/api/instances/${instanceId}/sync`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: "Bearer admintok",
    },
    credentials: "same-origin",
    body: JSON.stringify({ serverId }),
  });
  if (!res.ok) throw await parseError(res);
  return parseJSON(res);
}

export interface SecretStatus {
  exists: boolean;
  last4: string;
  updated_at: string;
}

export async function getSecretStatus(type: string): Promise<SecretStatus> {
  const res = await apiFetch(`/api/settings/secret/${type}/status`, {
    cache: "no-store",
    headers: { Authorization: "Bearer admintok" },
    credentials: "same-origin",
  });
  if (!res.ok) throw await parseError(res);
  return parseJSON(res);
}

export async function saveSecret(type: string, payload: any): Promise<void> {
  const res = await apiFetch(`/api/settings/secret/${type}`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: "Bearer admintok",
      "X-CSRF-Token": document.cookie.match(/csrf_token=([^;]+)/)?.[1] ?? "",
    },
    credentials: "same-origin",
    body: JSON.stringify(payload),
  });
  if (!res.ok) throw await parseError(res);
  window.dispatchEvent(new Event(`${type}-change`));
}

export async function clearSecret(type: string): Promise<void> {
  const res = await apiFetch(`/api/settings/secret/${type}`, {
    method: "DELETE",
    headers: {
      Authorization: "Bearer admintok",
      "X-CSRF-Token": document.cookie.match(/csrf_token=([^;]+)/)?.[1] ?? "",
    },
    credentials: "same-origin",
  });
  if (!res.ok) throw await parseError(res);
  window.dispatchEvent(new Event(`${type}-change`));
}

export async function testPuffer(): Promise<void> {
  const res = await apiFetch("/api/pufferpanel/test", {
    method: "POST",
    headers: {
      Authorization: "Bearer admintok",
    },
    credentials: "same-origin",
  });
  if (!res.ok) throw await parseError(res);
}
