import { create } from 'zustand';
import { getDashboard, startModUpdate } from '@/lib/api.ts';
import { emitDashboardRefresh } from '@/lib/refresh.js';

export const useDashboardStore = create((set, get) => ({
  data: null,
  loading: false,
  error: '',
  lastFetched: 0,
  refreshing: false,
  queued: false,
  fetch: async ({ force = false } = {}) => {
    const { lastFetched, loading, refreshing } = get();
    if (loading || refreshing) {
      if (force) set({ queued: true });
      return;
    }
    const now = Date.now();
    if (!force && now - lastFetched < 60000 && get().data) return;
    const hasData = !!get().data;
    set({ [hasData ? 'refreshing' : 'loading']: true, error: '' });
    try {
      const data = await getDashboard();
      set({ data, lastFetched: now });
    } catch (err) {
      set({ error: err instanceof Error ? err.message : 'Failed to load dashboard' });
    } finally {
      set({ [hasData ? 'refreshing' : 'loading']: false });
      if (get().queued) {
        set({ queued: false });
        await get().fetch({ force: true });
      }
    }
  },
  update: async (mod) => {
    const { data } = get();
    if (!data) return;
    const prev = structuredClone(data);
    set({
      data: {
        ...data,
        outdated: data.outdated - 1,
        up_to_date: data.up_to_date + 1,
        outdated_mods: data.outdated_mods.filter((m) => m.id !== mod.id),
        recent_updates: [
          { id: mod.id, name: mod.name || mod.url, version: mod.available_version, updated_at: new Date().toISOString() },
          ...data.recent_updates,
        ],
      },
    });
    try {
      const key = `${mod.id}:${mod.available_version || ''}:${crypto.randomUUID?.() || Math.random().toString(36).slice(2)}`;
      await startModUpdate(mod.id, key);
      emitDashboardRefresh({ force: true });
    } catch (err) {
      set({ data: prev });
      throw err;
    }
  },
}));
