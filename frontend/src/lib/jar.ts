export interface JarMeta {
  slug: string;
  id: string;
  version: string;
  mcVersion: string;
  loader: string;
  channel: string;
}

export function parseJarFilename(name: string): JarMeta {
  const meta: JarMeta = {
    slug: "",
    id: "",
    version: "",
    mcVersion: "",
    loader: "",
    channel: "",
  };
  name = name
    .toLowerCase()
    .replace(/\.jar$/, "")
    .replace(/[\[\]\(\)\{\}#]/g, "");
  const parts = name.split(/[-_+]/).filter(Boolean);
  if (parts.length === 0) return meta;
  const semver = /^v?\d+(?:\.\d+){1,3}[^a-zA-Z]*$/;
  const mcver = /^1\.\d+(?:\.\d+)?$/;
  const loaders = new Set(["fabric", "forge", "quilt", "neoforge"]);
  const channels = new Set(["beta", "alpha", "rc"]);
  const semvers: { idx: number; val: string }[] = [];
  for (let i = 0; i < parts.length; i++) {
    const p = parts[i];
    if (p.startsWith("mc") && mcver.test(p.slice(2))) {
      if (!meta.mcVersion) meta.mcVersion = p.slice(2);
      continue;
    }
    if (semver.test(p)) {
      semvers.push({ idx: i, val: p.replace(/^v/, "") });
      continue;
    }
    if (loaders.has(p)) {
      meta.loader = p;
      continue;
    }
    if (channels.has(p)) {
      meta.channel = p;
      continue;
    }
  }
  let verIdx = -1;
  if (semvers.length > 0) {
    const last = semvers[semvers.length - 1];
    verIdx = last.idx;
    meta.version = last.val;
    if (semvers.length > 1) {
      const prev = semvers[semvers.length - 2];
      if (mcver.test(last.val) && !mcver.test(prev.val)) {
        meta.version = prev.val;
        verIdx = prev.idx;
        meta.mcVersion = last.val;
      } else if (!meta.mcVersion) {
        for (const sv of semvers.slice(0, -1)) {
          if (mcver.test(sv.val)) {
            meta.mcVersion = sv.val;
            break;
          }
        }
      }
    }
  }
  const slugParts: string[] = [];
  for (let i = 0; i < parts.length; i++) {
    if (verIdx !== -1 && i >= verIdx) break;
    const p = parts[i];
    if (loaders.has(p) && i > 0) continue;
    if (p.startsWith("mc") && mcver.test(p.slice(2))) continue;
    if (mcver.test(p)) continue;
    if (channels.has(p) && i > 0) continue;
    slugParts.push(p);
  }
  meta.slug = slugParts.join("-");
  if (meta.slug) meta.id = meta.slug.split("-")[0];
  return meta;
}
