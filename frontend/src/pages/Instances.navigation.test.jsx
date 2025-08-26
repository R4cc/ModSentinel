import { render, screen, fireEvent, act } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi } from "vitest";
import { createMemoryRouter, RouterProvider } from "react-router-dom";

vi.mock("@/lib/api.ts", () => ({
  getInstances: vi.fn(),
  addInstance: vi.fn(),
  updateInstance: vi.fn(),
  deleteInstance: vi.fn(),
  getSecretStatus: vi.fn(),
  syncInstances: vi.fn(),
  getPufferServers: vi.fn(),
  getMods: vi.fn(),
  getInstance: vi.fn(),
  refreshMod: vi.fn(),
  deleteMod: vi.fn(),
  instances: { sync: vi.fn() },
}));

vi.mock("@/lib/toast.ts", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("focus-trap-react", () => ({ default: ({ children }) => children }));
const confirmMock = vi.fn();
vi.mock("@/hooks/useConfirm.jsx", () => ({
  useConfirm: () => ({ confirm: confirmMock, ConfirmModal: null }),
}));

import Instances from "./Instances.jsx";
import Mods from "./Mods.jsx";
import {
  getInstances,
  getSecretStatus,
  getMods,
  getInstance,
} from "@/lib/api.ts";

describe("Instance card navigation", () => {
  it("navigates to detail page and lists mods", async () => {
    getInstances.mockResolvedValue([
      {
        id: 1,
        name: "One",
        loader: "fabric",
        enforce_same_loader: true,
        mod_count: 1,
      },
    ]);
    getSecretStatus.mockResolvedValue({
      exists: true,
      last4: "",
      updated_at: "",
    });
    getInstance.mockResolvedValue({
      id: 1,
      name: "One",
      loader: "fabric",
      enforce_same_loader: true,
      mod_count: 1,
    });
    getMods.mockResolvedValue([
      {
        id: 1,
        name: "Alpha",
        url: "",
        game_version: "1.20",
        loader: "fabric",
        current_version: "1.0.0",
        available_version: "1.0.0",
        channel: "release",
        instance_id: 1,
      },
    ]);

    const router = createMemoryRouter(
      [
        { path: "/instances", element: <Instances /> },
        { path: "/instances/:id", element: <Mods /> },
      ],
      { initialEntries: ["/instances"] },
    );

    render(<RouterProvider router={router} />);

    const card = await screen.findByRole("link", { name: "One" });
    fireEvent.click(card);
    expect(router.state.location.pathname).toBe("/instances/1");
    expect(await screen.findByText("Alpha")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("link", { name: "Back to Instances" }));
    expect(router.state.location.pathname).toBe("/instances");
    expect(
      await screen.findByRole("link", { name: "One" }),
    ).toBeInTheDocument();
  });
});
