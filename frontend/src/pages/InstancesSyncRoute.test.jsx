import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi } from "vitest";
const navigate = vi.fn();
vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual("react-router-dom");
  return { ...actual, useNavigate: () => navigate };
});
import { MemoryRouter } from "react-router-dom";

vi.mock("@/lib/api.ts", async () => {
  const actual = await vi.importActual("@/lib/api.ts");
  return {
    ...actual,
    getInstances: vi.fn(),
    addInstance: vi.fn(),
    updateInstance: vi.fn(),
    deleteInstance: vi.fn(),
    getSecretStatus: vi.fn(),
    getMods: vi.fn(),
    // use real getPufferServers
  };
});

vi.mock("focus-trap-react", () => ({
  default: ({ children }) => children,
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import Instances from "./Instances.jsx";
import { getInstances, getSecretStatus } from "@/lib/api.ts";

describe("Instances PufferPanel fetch", () => {
  it("fetches servers via /api/instances/sync", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify([]), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    const origFetch = global.fetch;
    // @ts-ignore
    global.fetch = fetchMock;

    getInstances.mockResolvedValueOnce([]);
    getSecretStatus.mockResolvedValue({
      exists: true,
      last4: "1234",
      updated_at: "",
    });

    render(
      <MemoryRouter>
        <Instances />
      </MemoryRouter>,
    );
    await waitFor(() =>
      expect(getSecretStatus).toHaveBeenCalledWith("pufferpanel"),
    );
    const addBtn = await screen.findByRole("button", { name: /add instance/i });
    fireEvent.click(addBtn);

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());

    const urls = fetchMock.mock.calls.map((c) => c[0]);
    expect(urls).toContain(`${window.location.origin}/api/instances/sync`);
    expect(
      urls.some((u) => u.startsWith(`${window.location.origin}/api/pufferpanel/`)),
    ).toBe(false);

    // @ts-ignore
    global.fetch = origFetch;
  });
});
