import { describe, it, expect, vi, afterEach } from "vitest";
import { getModMetadata, getPufferServers } from "./api";

afterEach(() => {
  // @ts-ignore
  global.fetch = originalFetch;
});

// Preserve original fetch
// @ts-ignore
const originalFetch = global.fetch;

describe("proxy API calls", () => {
  it("does not send tokens when fetching mod metadata", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ versions: [] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    // @ts-ignore
    global.fetch = fetchMock;

    await getModMetadata("https://example.com/mod");

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, opts] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/mods/metadata");
    expect(opts.method).toBe("POST");
    expect(opts.headers?.Authorization).toBeUndefined();
    expect(String(opts.body)).not.toContain("token");
  });

  it("sends token when listing PufferPanel servers", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify([]), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    // @ts-ignore
    global.fetch = fetchMock;

    await getPufferServers();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, opts] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/instances/sync");
    expect(opts?.method).toBe("POST");
    expect(opts?.headers?.Authorization).toBe("Bearer admintok");
    expect(opts?.credentials).toBe("same-origin");
  });
});

describe("safe JSON parsing", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("handles empty success body", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response(null, {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        ),
    );
    // @ts-ignore
    await expect(getPufferServers()).resolves.toBeUndefined();
  });

  it("handles invalid json success body", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response("oops", {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        ),
    );
    // @ts-ignore
    await expect(getPufferServers()).resolves.toBeUndefined();
  });

  it("handles empty error body", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(null, { status: 400 })),
    );
    // @ts-ignore
    await expect(getPufferServers()).rejects.toThrow("400");
  });

  it("handles non-json error body", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response("oops", { status: 500 })),
    );
    // @ts-ignore
    await expect(getPufferServers()).rejects.toThrow("500 oops");
  });

  it("uses server message and request id", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            code: "bad_request",
            message: "bad",
            requestId: "abc123",
          }),
          { status: 400, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );
    // @ts-ignore
    await getPufferServers().catch((err) => {
      expect(err.message).toBe("bad");
      // @ts-ignore
      expect(err.requestId).toBe("abc123");
    });
  });
});
