import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, beforeEach } from "vitest";

vi.mock("focus-trap-react", () => ({
  default: ({ children }) => children,
}));

vi.mock("@/lib/api.ts", () => ({
  getMods: vi.fn(),
  refreshMod: vi.fn(),
  deleteMod: vi.fn(),
  getToken: vi.fn(),
  getInstance: vi.fn(),
  updateInstance: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

const confirmMock = vi.fn();
vi.mock("@/hooks/useConfirm.jsx", () => ({
  useConfirm: () => ({ confirm: confirmMock, ConfirmModal: null }),
}));

import Mods from "./Mods.jsx";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import {
  getMods,
  getInstance,
  updateInstance,
  getToken,
  refreshMod,
  deleteMod,
} from "@/lib/api.ts";

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/instances/1"]}>
      <Routes>
        <Route path="/instances/:id" element={<Mods />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("Mods instance editing", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getToken.mockResolvedValue("token");
    confirmMock.mockResolvedValue(true);
    getMods.mockResolvedValue([]);
    getInstance.mockResolvedValue({
      id: 1,
      name: "Old",
      loader: "fabric",
      enforce_same_loader: true,
      created_at: "",
      mod_count: 0,
    });
  });

  it("renames and toggles enforcement", async () => {
    updateInstance
      .mockResolvedValueOnce({
        id: 1,
        name: "New",
        loader: "fabric",
        enforce_same_loader: true,
        created_at: "",
        mod_count: 0,
      })
      .mockResolvedValueOnce({
        id: 1,
        name: "New",
        loader: "fabric",
        enforce_same_loader: false,
        created_at: "",
        mod_count: 0,
      });

    renderPage();

    expect(
      await screen.findByRole("heading", { name: /Old/ }),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByLabelText("Rename instance"));
    const input = screen.getByDisplayValue("Old");
    fireEvent.change(input, { target: { value: "New" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(updateInstance).toHaveBeenCalledWith(1, {
        name: "New",
        loader: "fabric",
        enforce_same_loader: true,
      }),
    );
    expect(screen.getByRole("heading", { name: /New/ })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    const checkbox = screen.getByLabelText("Enforce same loader for mods");
    fireEvent.click(checkbox);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(updateInstance).toHaveBeenLastCalledWith(1, {
        name: "New",
        loader: "fabric",
        enforce_same_loader: false,
      }),
    );
  });
});

describe("Mods instance scoping", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getToken.mockResolvedValue("token");
    confirmMock.mockResolvedValue(true);
    getInstance.mockResolvedValue({
      id: 1,
      name: "Inst",
      loader: "fabric",
      enforce_same_loader: true,
      created_at: "",
      mod_count: 0,
    });
    const modA = {
      id: 1,
      name: "Alpha",
      url: "https://example.com/a",
      game_version: "1.20",
      loader: "fabric",
      current_version: "1.0",
      available_version: "1.0",
      channel: "release",
      instance_id: 1,
    };
    getMods.mockResolvedValue([modA]);
    refreshMod.mockResolvedValue([modA]);
    deleteMod.mockResolvedValue([]);
  });

  it("shows loader in header and loads mods for the instance", async () => {
    renderPage();
    await waitFor(() => expect(getMods).toHaveBeenCalledWith(1));
    const heading = await screen.findByRole("heading", { name: /Inst/ });
    expect(heading).toHaveTextContent("Inst");
    expect(heading).toHaveTextContent("fabric");
    expect(screen.getByText("Alpha")).toBeInTheDocument();
  });

  it("handles refresh and delete within the instance", async () => {
    renderPage();
    fireEvent.click(
      await screen.findByLabelText("Check for updates"),
    );
    await waitFor(() =>
      expect(refreshMod).toHaveBeenCalledWith(
        1,
        expect.objectContaining({ instance_id: 1 }),
      ),
    );
    fireEvent.click(screen.getAllByLabelText("Delete mod")[0]);
    await waitFor(() => expect(deleteMod).toHaveBeenCalledWith(1, 1));
  });
});
