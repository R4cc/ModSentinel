import { toast as sonnerToast } from "sonner";

export type ToastKind = "info" | "success" | "error" | "warning";

interface ToastOptions {
  id?: number | string;
  title?: string;
  body: string;
  kind?: ToastKind;
  duration?: number;
  key?: string;
}

interface QueueItem extends ToastOptions {
  count: number;
  timestamp?: number;
}

const queue: QueueItem[] = [];
const active: QueueItem[] = [];
const maxVisible = 3;
const duplicateWindowMs = 5000;

function renderNext() {
  if (active.length >= maxVisible || queue.length === 0) return;
  const next = queue.shift()!;
  const id = sonnerToast[next.kind ?? "info"](next.body, {
    id: next.id,
    duration: next.duration,
  });
  active.push({ ...next, id });
}

function enqueue(item: ToastOptions) {
  const now = Date.now();
  if (item.key) {
    const existingActive = active.find((t) => t.key === item.key);
    if (existingActive && now - existingActive.timestamp! < duplicateWindowMs) {
      existingActive.count++;
      sonnerToast.dismiss(existingActive.id!);
      const id = sonnerToast[existingActive.kind ?? "info"](
        `${existingActive.body} (${existingActive.count})`,
        { id: existingActive.id, duration: existingActive.duration },
      );
      existingActive.id = id;
      return;
    }
    const existingQueued = queue.find((t) => t.key === item.key);
    if (existingQueued && now - existingQueued.timestamp! < duplicateWindowMs) {
      existingQueued.body = item.body;
      return;
    }
  }
  const q: QueueItem = { ...item, count: 1, timestamp: now } as QueueItem & {
    timestamp: number;
  };
  queue.push(q);
  renderNext();
}

function show(opts: ToastOptions) {
  enqueue(opts);
}

function dismiss(id: number | string) {
  sonnerToast.dismiss(id);
  const idx = active.findIndex((t) => t.id === id);
  if (idx !== -1) {
    active.splice(idx, 1);
    renderNext();
  }
}

function clear() {
  active.forEach((t) => sonnerToast.dismiss(t.id!));
  active.length = 0;
  queue.length = 0;
}

export const toast = {
  show,
  dismiss,
  clear,
  success(body: string, opts: Omit<ToastOptions, "body" | "kind"> = {}) {
    show({ ...opts, body, kind: "success" });
  },
  error(body: string, opts: Omit<ToastOptions, "body" | "kind"> = {}) {
    show({ ...opts, body, kind: "error" });
  },
  info(body: string, opts: Omit<ToastOptions, "body" | "kind"> = {}) {
    show({ ...opts, body, kind: "info" });
  },
  warning(body: string, opts: Omit<ToastOptions, "body" | "kind"> = {}) {
    show({ ...opts, body, kind: "warning" });
  },
};

export default toast;
