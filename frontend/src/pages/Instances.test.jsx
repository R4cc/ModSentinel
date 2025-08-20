import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, beforeEach } from "vitest";

const navigate = vi.fn();
vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual("react-router-dom");
  return { ...actual, useNavigate: () => navigate };
});

vi.mock("@/lib/api.ts", () => ({
  getInstances: vi.fn(),
  addInstance: vi.fn(),
  updateInstance: vi.fn(),
  deleteInstance: vi.fn(),
  getPufferCreds: vi.fn(),
  syncInstances: vi.fn(),
  getPufferServers: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("focus-trap-react", () => ({
  default: ({ children }) => children,
}));

import { MemoryRouter } from "react-router-dom";
import Instances from "./Instances.jsx";
import {
  getInstances,
  addInstance,
  updateInstance,
  deleteInstance,
  getPufferCreds,
  syncInstances,
  getPufferServers,
} from "@/lib/api.ts";
import { toast } from "sonner";

describe("Instances page", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getPufferCreds.mockResolvedValue({
      base_url: "",
      client_id: "",
      client_secret: "",
    });
  });

  it("shows empty state when no instances", async () => {
    getInstances.mockResolvedValue([]);
    render(
      <MemoryRouter>
        <Instances />
      </MemoryRouter>,
    );
    expect(await screen.findByText("No instances")).toBeInTheDocument();
  });

  it("shows error state on failure", async () => {
    getInstances.mockRejectedValueOnce(new Error("oops"));
    render(
      <MemoryRouter>
        <Instances />
      </MemoryRouter>,
    );
    expect(await screen.findByText("oops")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });

  it("redirects to single instance", async () => {
    getInstances.mockResolvedValueOnce([
      { id: 1, name: "Default", loader: "fabric", enforce_same_loader: true },
    ]);
    render(
      <MemoryRouter initialEntries={["/instances"]}>
        <Instances />
      </MemoryRouter>,
    );
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith("/instances/1", { replace: true }),
    );
  });

  it("creates instance and navigates", async () => {
    getInstances.mockResolvedValueOnce([]);
    const promise = Promise.resolve({
      id: 1,
      name: "Test",
      loader: "fabric",
      enforce_same_loader: true,
    });
    addInstance.mockReturnValue(promise);

    render(
      <MemoryRouter>
        <Instances />
      </MemoryRouter>,
    );

    const [newBtn] = await screen.findAllByRole("button", {
      name: /new instance/i,
    });
    fireEvent.click(newBtn);
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Test" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));

    expect(screen.getByText("Test")).toBeInTheDocument();

    await promise;
    await waitFor(() =>
      expect(addInstance).toHaveBeenCalledWith({
        name: "Test",
        loader: "fabric",
        enforce_same_loader: true,
      }),
    );
    expect(toast.success).toHaveBeenCalledWith("Instance added");
    expect(navigate).toHaveBeenCalledWith("/instances/1");
  });

  it("validates name is required", async () => {
    getInstances.mockResolvedValueOnce([]);
    render(
      <MemoryRouter>
        <Instances />
      </MemoryRouter>,
    );

    const [newBtn] = await screen.findAllByRole("button", {
      name: /new instance/i,
    });
    fireEvent.click(newBtn);
    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    expect(addInstance).not.toHaveBeenCalled();
    expect(toast.error).toHaveBeenCalledWith("Name required");
  });

  it("creates instance with enforcement disabled", async () => {
    getInstances.mockResolvedValueOnce([]);
    const promise = Promise.resolve({
      id: 1,
      name: "Test",
      loader: "fabric",
      enforce_same_loader: false,
    });
    addInstance.mockReturnValue(promise);

    render(
      <MemoryRouter>
        <Instances />
      </MemoryRouter>,
    );

    const [newBtn] = await screen.findAllByRole("button", {
      name: /new instance/i,
    });
    fireEvent.click(newBtn);
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Test" },
    });
    fireEvent.click(screen.getByLabelText("Enforce same loader for mods"));
    fireEvent.click(screen.getByRole("button", { name: "Add" }));

    await promise;
    await waitFor(() =>
      expect(addInstance).toHaveBeenCalledWith({
        name: "Test",
        loader: "fabric",
        enforce_same_loader: false,
      }),
    );
  });

  it("updates instance and toggles enforcement", async () => {
    getInstances.mockResolvedValueOnce([
      {
        id: 1,
        name: "One",
        loader: "fabric",
        enforce_same_loader: true,
        mod_count: 0,
      },
      {
        id: 2,
        name: "Two",
        loader: "forge",
        enforce_same_loader: true,
        mod_count: 0,
      },
    ]);
    updateInstance.mockResolvedValue({
      id: 1,
      name: "One!",
      loader: "fabric",
      enforce_same_loader: true,
    });

    render(
      <MemoryRouter>
        <Instances />
      </MemoryRouter>,
    );

    const [editBtn] = await screen.findAllByRole("button", { name: "Edit" });
    fireEvent.click(editBtn);
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "One!" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(updateInstance).toHaveBeenCalledWith(1, {
        name: "One!",
      }),
    );
    expect(toast.success).toHaveBeenCalledWith("Instance updated");
  });

  it("shows sync button when pufferpanel credentials exist", async () => {
    getInstances.mockResolvedValueOnce([]);
    getPufferCreds.mockResolvedValueOnce({
      base_url: "url",
      client_id: "id",
      client_secret: "secret",
    });
    render(
      <MemoryRouter>
        <Instances />
      </MemoryRouter>,
    );
    expect(
      await screen.findByRole("button", { name: /sync/i }),
    ).toBeInTheDocument();
  });

  it("disables PufferPanel sync without credentials", async () => {
    getInstances.mockResolvedValueOnce([]);
    render(
      <MemoryRouter>
        <Instances />
      </MemoryRouter>,
    );
    const [newBtn] = await screen.findAllByRole("button", {
      name: /new instance/i,
    });
    fireEvent.click(newBtn);
    const toggle = screen.getByLabelText("Sync from PufferPanel");
    expect(toggle).toBeDisabled();
    expect(screen.getByText(/pufferpanel credentials/i)).toBeInTheDocument();
  });

  it("enables create after selecting server", async () => {
    getInstances.mockResolvedValueOnce([]);
    getPufferCreds.mockResolvedValueOnce({
      base_url: "url",
      client_id: "id",
      client_secret: "secret",
    });
    getPufferServers.mockResolvedValueOnce([{ id: "1", name: "One" }]);
    syncInstances.mockResolvedValueOnce({
      instance: {
        id: 1,
        name: "Srv",
        loader: "fabric",
        enforce_same_loader: true,
        mod_count: 0,
      },
      unmatched: [],
      mods: [],
    });
    render(
      <MemoryRouter>
        <Instances />
      </MemoryRouter>,
    );
    const [newBtn] = await screen.findAllByRole("button", {
      name: /new instance/i,
    });
    fireEvent.click(newBtn);
    const toggle = screen.getByLabelText("Sync from PufferPanel");
    fireEvent.click(toggle);
    const select = await screen.findByLabelText("Server");
    const add = screen.getByRole("button", { name: "Add" });
    expect(add).toBeDisabled();
    fireEvent.change(select, { target: { value: "1" } });
    expect(add).not.toBeDisabled();
    fireEvent.click(add);
    await waitFor(() => expect(syncInstances).toHaveBeenCalledWith("1"));
  });

  it("shows scanning progress during sync", async () => {
    getInstances.mockResolvedValueOnce([]);
    getPufferCreds.mockResolvedValueOnce({
      base_url: "url",
      client_id: "id",
      client_secret: "secret",
    });
    getPufferServers.mockResolvedValueOnce([{ id: "1", name: "One" }]);
    let resolveSync;
    syncInstances.mockReturnValueOnce(
      new Promise((res) => {
        resolveSync = res;
      }),
    );
    render(
      <MemoryRouter>
        <Instances />
      </MemoryRouter>,
    );
    const [newBtn] = await screen.findAllByRole("button", {
      name: /new instance/i,
    });
    fireEvent.click(newBtn);
    fireEvent.click(screen.getByLabelText("Sync from PufferPanel"));
    const select = await screen.findByLabelText("Server");
    fireEvent.change(select, { target: { value: "1" } });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    expect(screen.getByText(/scanning/i)).toBeInTheDocument();
    resolveSync({
      instance: {
        id: 1,
        name: "Srv",
        loader: "fabric",
        enforce_same_loader: true,
        mod_count: 0,
      },
      unmatched: ["a.jar"],
      mods: [],
    });
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith("/instances/1", {
        state: { unmatched: ["a.jar"], mods: [] },
      }),
    );
  });
});
