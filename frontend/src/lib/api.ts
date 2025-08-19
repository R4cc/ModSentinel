export interface ModVersion {
  id: string;
  version_number: string;
  version_type: string;
  date_published: string;
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

export async function getMinecraftVersions(): Promise<string[]> {
  const res = await fetch('https://launchermeta.mojang.com/mc/game/version_manifest.json');
  if (!res.ok) throw new Error('Failed to fetch Minecraft versions');
  const data: { versions: { id: string }[] } = await res.json();
  const ids = data.versions.map((v) => v.id);
  ids.sort((a, b) => b.localeCompare(a, undefined, { numeric: true }));
  return ids;
}

export async function getModVersions(slug: string, loader: string, mcVersion: string): Promise<ModVersion[]> {
  const params = new URLSearchParams({
    loaders: JSON.stringify([loader]),
    game_versions: JSON.stringify([mcVersion]),
  });
  const res = await fetch(`https://api.modrinth.com/v2/project/${slug}/version?${params}`);
  if (!res.ok) throw new Error('Failed to fetch mod versions');
  const data: ModVersion[] = await res.json();
  return data;
}

export async function getMods(): Promise<Mod[]> {
  const res = await fetch('/api/mods');
  if (!res.ok) throw new Error('Failed to fetch mods');
  return res.json();
}

export interface NewMod {
  url: string;
  game_version: string;
  loader: string;
  channel: string;
}

export async function addMod(payload: NewMod): Promise<Mod[]> {
  const res = await fetch('/api/mods', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!res.ok) throw new Error('Failed to add mod');
  return res.json();
}

export async function refreshMod(id: number, payload: NewMod): Promise<Mod[]> {
  const res = await fetch(`/api/mods/${id}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!res.ok) throw new Error('Failed to update mod');
  return res.json();
}

export async function deleteMod(id: number): Promise<Mod[]> {
  const res = await fetch(`/api/mods/${id}`, { method: 'DELETE' });
  if (!res.ok) throw new Error('Failed to delete mod');
  return res.json();
}

export function slugFromUrl(url: string): string | null {
  try {
    const u = new URL(url);
    if (u.hostname.includes('modrinth.com')) {
      const parts = u.pathname.split('/').filter(Boolean);
      const idx = parts.findIndex((p) => p === 'mod');
      return idx >= 0 ? parts[idx + 1] : null;
    }
  } catch {
    return null;
  }
  return null;
}

