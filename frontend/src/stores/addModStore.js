import { create } from 'zustand';
import { toast } from 'sonner';
import { getMinecraftVersions, getModVersions, slugFromUrl } from '@/lib/api.ts';

export const useAddModStore = create((set, get) => ({
  step: 0,
  url: '',
  urlError: '',
  loader: '',
  mcVersion: '',
  versions: [],
  loadingVersions: false,
  modVersions: [],
  loadingModVersions: false,
  includePre: false,
  selectedModVersion: null,
  setUrl: (url) => set({ url }),
  validateUrl: (value) => {
    try {
      new URL(value);
      set({ urlError: '' });
    } catch {
      set({ urlError: 'Enter a valid URL' });
    }
  },
  setLoader: (loader) => set({ loader }),
  setMcVersion: (mcVersion) => set({ mcVersion }),
  setIncludePre: (includePre) => set({ includePre }),
  setSelectedModVersion: (id) => set({ selectedModVersion: id }),
  nextStep: () => set((s) => ({ step: Math.min(3, s.step + 1) })),
  prevStep: () => set((s) => ({ step: Math.max(0, s.step - 1) })),
  fetchVersions: async () => {
    set({ loadingVersions: true });
    try {
      const versions = await getMinecraftVersions();
      set({ versions });
    } catch {
      toast.error('Failed to load versions');
    } finally {
      set({ loadingVersions: false });
    }
  },
  fetchModVersions: async () => {
    const { url, loader, mcVersion } = get();
    const slug = slugFromUrl(url);
    if (!slug) {
      toast.error('Cannot parse mod URL');
      return;
    }
    set({ loadingModVersions: true });
    try {
      const mods = await getModVersions(slug, loader, mcVersion);
      set({ modVersions: mods });
    } catch {
      toast.error('Failed to load mod versions');
    } finally {
      set({ loadingModVersions: false });
    }
  },
}));

