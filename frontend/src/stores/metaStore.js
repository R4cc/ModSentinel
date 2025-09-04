import { create } from 'zustand';

export const useMetaStore = create((set, get) => ({
  loaders: [],
  loading: false,
  loaded: false,
  error: '',
  async load() {
    if (get().loading) return;
    set({ loading: true, error: '' });
    try {
      const res = await fetch('/api/meta/modrinth/loaders', { cache: 'no-store' });
      if (!res.ok) throw new Error('Failed to load loaders');
      const data = await res.json();
      const items = Array.isArray(data) ? data : [];
      set({ loaders: items, loaded: true, loading: false, error: '' });
      return true;
    } catch (e) {
      set({ loading: false, error: e instanceof Error ? e.message : 'Failed to load loaders' });
      return false;
    }
  },
}));

