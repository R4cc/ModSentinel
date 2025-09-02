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
    try {
      const key = `${mod.id}:${mod.available_version || ''}:${crypto.randomUUID?.() || Math.random().toString(36).slice(2)}`;
      await startModUpdate(mod.id, key);
      // Do not update dashboard optimistically; wait for worker to finish and refresh
      emitDashboardRefresh({ force: true });
    } catch (err) {
      throw err;
    }
  },
}));
