export function parseJarFilename(name: string): { slug: string; version: string } {
  let slug = '';
  let version = '';
  name = name.toLowerCase().replace(/\.jar$/, '');
  const parts = name.split(/[-_]/).filter(Boolean);
  if (parts.length === 0) return { slug: '', version: '' };
  let idx = -1;
  for (let i = 1; i < parts.length; i++) {
    if (/^[0-9]/.test(parts[i])) {
      idx = i;
      break;
    }
  }
  let slugParts = parts;
  if (idx !== -1) {
    version = parts[idx];
    slugParts = parts.slice(0, idx);
  }
  const loaders = new Set(['fabric', 'forge', 'quilt', 'neoforge']);
  slugParts = slugParts.filter((p) => !loaders.has(p) && !p.startsWith('mc'));
  if (slugParts.length === 0) slugParts = parts.slice(0, idx === -1 ? parts.length : idx);
  slug = slugParts.join('-');
  return { slug, version };
}
