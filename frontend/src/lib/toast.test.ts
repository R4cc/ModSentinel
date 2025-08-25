import { describe, it, expect, vi, beforeEach } from "vitest";

let id = 0;
vi.mock("sonner", () => {
  const base: any = vi.fn(() => ++id);
  base.info = vi.fn(() => ++id);
  base.success = vi.fn(() => ++id);
  base.error = vi.fn(() => ++id);
  base.warning = vi.fn(() => ++id);
  base.dismiss = vi.fn();
  return { toast: base };
});

import { toast } from "./toast.ts";
import { toast as sonnerToast } from "sonner";

beforeEach(() => {
  id = 0;
  toast.clear();
  (sonnerToast.info as any).mockClear();
  (sonnerToast.dismiss as any).mockClear();
});

describe("toast queue", () => {
  it("queues beyond maxVisible", () => {
    toast.info("a");
    toast.info("b");
    toast.info("c");
    toast.info("d");
    expect((sonnerToast.info as any)).toHaveBeenCalledTimes(3);
    toast.dismiss(1);
    expect((sonnerToast.info as any)).toHaveBeenCalledTimes(4);
  });

  it("coalesces duplicate keys", () => {
    toast.show({ body: "first", key: "dup" });
    toast.show({ body: "second", key: "dup" });
    expect((sonnerToast.info as any)).toHaveBeenCalledTimes(2);
    expect((sonnerToast.dismiss as any)).toHaveBeenCalledTimes(1);
  });

  it("clears active toasts", () => {
    toast.info("one");
    toast.info("two");
    (sonnerToast.dismiss as any).mockClear();
    toast.clear();
    expect((sonnerToast.dismiss as any)).toHaveBeenCalledTimes(2);
  });
});
