import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

vi.mock('@/lib/api.ts', () => ({
  getDashboard: vi.fn(),
  updateModVersion: vi.fn(),
}));
vi.mock('@/lib/refresh.js', () => ({
  emitDashboardRefresh: vi.fn(),
}));

import { useDashboardStore } from './dashboardStore.js';

beforeEach(() => {
  useDashboardStore.setState({
    data: null,
    loading: false,
    error: '',
    lastFetched: 0,
    refreshing: false,
    queued: false,
  });
  vi.clearAllMocks();
});

afterEach(() => {
  vi.useRealTimers();
});

describe('dashboard store', () => {
  it('caches dashboard data for 60 seconds', async () => {
    const { getDashboard } = await import('@/lib/api.ts');
    getDashboard.mockResolvedValue({ outdated: 0 });
    vi.useFakeTimers();

    await useDashboardStore.getState().fetch();
    expect(getDashboard).toHaveBeenCalledTimes(1);

    await useDashboardStore.getState().fetch();
    expect(getDashboard).toHaveBeenCalledTimes(1);

    vi.advanceTimersByTime(60000);
    await useDashboardStore.getState().fetch();
    expect(getDashboard).toHaveBeenCalledTimes(2);
  });

  it('rolls back failed mod updates', async () => {
    const mod = { id: '1', name: 'Test', available_version: '2' };
    useDashboardStore.setState({
      data: {
        outdated: 1,
        up_to_date: 0,
        outdated_mods: [mod],
        recent_updates: [],
      },
    });
    const { updateModVersion } = await import('@/lib/api.ts');
    updateModVersion.mockRejectedValue(new Error('fail'));

    await expect(useDashboardStore.getState().update(mod)).rejects.toBeDefined();

    const state = useDashboardStore.getState().data;
    expect(state.outdated).toBe(1);
    expect(state.outdated_mods).toEqual([mod]);
    expect(state.recent_updates).toEqual([]);
  });
});
