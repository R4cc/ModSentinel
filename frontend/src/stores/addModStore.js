import { create } from 'zustand';
import { toast } from '@/lib/toast.ts';
import { getModMetadata } from '@/lib/api.ts';

function parseModKey(url) {
  try {
    const u = new URL(url);
    const parts = u.pathname.split('/');
    const idx = parts.findIndex((p) =>
      ['mod', 'plugin', 'datapack', 'resourcepack'].includes(p),
    );
    if (idx !== -1 && parts[idx + 1]) {
      return { source: u.hostname, modId: parts[idx + 1] };
    }
  } catch {}
  return { source: '', modId: '' };
}

export function initialState() {
  return {
    step: 0,
    url: '',
    urlError: '',
    loader: '',
    mcVersion: '',
    versions: [],
    loadingVersions: false,
    allModVersions: [],
    modVersions: [],
    loadingModVersions: false,
    includePre: false,
    selectedModVersion: null,
    versionCache: {},
    modVersionCache: {},
  };
}

export const useAddModStore = create((set, get) => ({
  ...initialState(),
  nonce: 0,
  setUrl: (url) => set({ url }),
  validateUrl: (value) => {
    try {
      new URL(value);
      set({ urlError: '' });
      return true;
    } catch {
      set({ urlError: 'Enter a valid URL' });
      return false;
    }
  },
  setLoader: (loader) => set({ loader }),
  setMcVersion: (mcVersion) => set({ mcVersion }),
  setIncludePre: (includePre) => set({ includePre }),
  setSelectedModVersion: (id) => set({ selectedModVersion: id }),
  nextStep: () => set((s) => ({ step: Math.min(3, s.step + 1) })),
  prevStep: () => set((s) => ({ step: Math.max(0, s.step - 1) })),
  fetchVersions: async () => {
    const { url, versionCache } = get();
    const { source, modId } = parseModKey(url);
    const key = `${source}:${modId}`;
    const cached = versionCache[key];
    if (cached) {
      set({
        versions: cached.versions ?? [],
        allModVersions: cached.allModVersions ?? [],
      });
      return;
    }
    set({ loadingVersions: true });
    try {
      const meta = await getModMetadata(url);
      set({
        versions: meta.game_versions,
        allModVersions: meta.versions,
        versionCache: {
          ...versionCache,
          [key]: { versions: meta.game_versions, allModVersions: meta.versions },
        },
      });
    } catch (err) {
      if (err instanceof Error && err.message === 'token required') {
        toast.error('Modrinth token required');
      } else {
        toast.error('Failed to load versions');
      }
    } finally {
      set({ loadingVersions: false });
    }
  },
  fetchModVersions: async () => {
    const { loader, mcVersion, allModVersions, url, modVersionCache } = get();
    const { source, modId } = parseModKey(url);
    const key = `${source}:${modId}:${loader}:${mcVersion}`;
    const cached = modVersionCache[key];
    if (cached) {
      set({ modVersions: cached });
      return;
    }
    set({ loadingModVersions: true });
    try {
      const mods = (allModVersions ?? []).filter(
        (v) =>
          v.loaders.includes(loader) && v.game_versions.includes(mcVersion),
      );
      set({
        modVersions: mods,
        modVersionCache: { ...modVersionCache, [key]: mods },
      });
    } finally {
      set({ loadingModVersions: false });
    }
  },
  resetWizard: () => {
    const next = { ...initialState(), nonce: get().nonce + 1 };
    set(next);
    return next;
  },
}));
