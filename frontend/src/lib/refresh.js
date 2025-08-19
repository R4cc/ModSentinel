const listeners = new Set();

export function onDashboardRefresh(fn) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

export function emitDashboardRefresh(options) {
  for (const fn of listeners) {
    fn(options);
  }
}
