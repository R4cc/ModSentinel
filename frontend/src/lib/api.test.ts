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
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ versions: [] }),
    });
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

  it("does not send tokens when listing PufferPanel servers", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve([]),
    });
    // @ts-ignore
    global.fetch = fetchMock;

    await getPufferServers();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, opts] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/pufferpanel/servers");
    expect(opts?.headers?.Authorization).toBeUndefined();
  });
});
