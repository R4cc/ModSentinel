import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { axe } from "vitest-axe";

HTMLCanvasElement.prototype.getContext = () => {};

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
  getSecretStatus: vi.fn(),
  syncInstances: vi.fn(),
  getPufferServers: vi.fn(),
  getMods: vi.fn(),
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
  getSecretStatus,
  syncInstances,
  getPufferServers,
  getMods,
} from "@/lib/api.ts";
import { toast } from "sonner";

describe("Instances page", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getSecretStatus.mockResolvedValue({
      exists: true,
      last4: "",
      updated_at: "",
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
    const [addBtn] = await screen.findAllByRole("button", {
      name: /add instance/i,
    });
    expect(addBtn).toBeInTheDocument();
  });

  it("shows token and puffer warnings", async () => {
    getSecretStatus.mockResolvedValueOnce({
      exists: false,
      last4: "",
      updated_at: "",
    });
    getSecretStatus.mockResolvedValueOnce({
      exists: false,
      last4: "",
      updated_at: "",
    });
    getInstances.mockResolvedValue([]);
    render(
      <MemoryRouter>
        <Instances />
      </MemoryRouter>,
    );
    expect(
      await screen.findByText(
        "Set a Modrinth token in Settings to enable update checks.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getAllByText(
        "Set PufferPanel credentials in Settings to enable sync.",
      ).length,
    ).toBeGreaterThan(0);
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

  it("shows overview without loading mods or redirecting when single instance", async () => {
    getInstances.mockResolvedValueOnce([
      {
        id: 1,
        name: "Default",
        loader: "fabric",
        enforce_same_loader: true,
        mod_count: 0,
      },
    ]);
    render(
      <MemoryRouter initialEntries={["/instances"]}>
        <Instances />
      </MemoryRouter>,
    );
    expect(await screen.findByText("Default")).toBeInTheDocument();
    const [addBtn] = await screen.findAllByRole("button", {
      name: /add instance/i,
    });
    expect(addBtn).toBeInTheDocument();
    const grid = await screen.findByTestId("instance-grid");
    expect(grid.children).toHaveLength(1);
    expect(grid).toHaveClass("grid-cols-1 sm:grid-cols-2 lg:grid-cols-3", {
      exact: false,
    });
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(navigate).not.toHaveBeenCalled();
    expect(getMods).not.toHaveBeenCalled();
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

    const [addBtn] = await screen.findAllByRole("button", {
      name: /add instance/i,
    });
    fireEvent.click(addBtn);
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

    const [addBtn] = await screen.findAllByRole("button", {
      name: /add instance/i,
    });
    fireEvent.click(addBtn);
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

    const [addBtn] = await screen.findAllByRole("button", {
      name: /add instance/i,
    });
    fireEvent.click(addBtn);
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Test" },
    });
    fireEvent.change(screen.getByLabelText("Loader"), {
      target: { value: "forge" },
    });
    fireEvent.click(screen.getByLabelText("Enforce same loader for mods"));
    fireEvent.click(screen.getByRole("button", { name: "Add" }));

    await promise;
    await waitFor(() =>
      expect(addInstance).toHaveBeenCalledWith({
        name: "Test",
        loader: "forge",
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
    getSecretStatus.mockResolvedValueOnce({
      exists: true,
      last4: "1234",
      updated_at: "",
    });
    render(
      <MemoryRouter>
        <Instances />
      </MemoryRouter>,
    );
    const syncBtns = await screen.findAllByRole("button", { name: /sync/i });
    expect(syncBtns.length).toBeGreaterThan(0);
  });

  it("disables PufferPanel sync without credentials", async () => {
    getInstances.mockResolvedValueOnce([]);
    getSecretStatus.mockResolvedValue({
      exists: false,
      last4: "",
      updated_at: "",
    });
    render(
      <MemoryRouter>
        <Instances />
      </MemoryRouter>,
    );
    const [addBtn] = await screen.findAllByRole("button", {
      name: /add instance/i,
    });
    fireEvent.click(addBtn);
    await screen.findByLabelText("Sync from PufferPanel");
    expect(getPufferServers).not.toHaveBeenCalled();
    expect(
      screen.getAllByText(/pufferpanel credentials/i).length,
    ).toBeGreaterThan(0);
  });

  it("enables create after selecting server", async () => {
    getInstances.mockResolvedValueOnce([]);
    getSecretStatus.mockResolvedValueOnce({
      exists: true,
      last4: "1234",
      updated_at: "",
    });
    getPufferServers.mockResolvedValueOnce([{ id: "1", name: "One" }]);
    addInstance.mockResolvedValueOnce({
      id: 1,
      name: "",
      loader: "",
      enforce_same_loader: true,
      pufferpanel_server_id: "1",
      mod_count: 0,
    });
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
    const [addBtn] = await screen.findAllByRole("button", {
      name: /add instance/i,
    });
    fireEvent.click(addBtn);
    const toggle = screen.getByLabelText("Sync from PufferPanel");
    fireEvent.click(toggle);
    const select = await screen.findByLabelText("Server");
    const add = screen.getByRole("button", { name: "Add" });
    expect(add).toBeDisabled();
    fireEvent.change(select, { target: { value: "1" } });
    expect(add).not.toBeDisabled();
    fireEvent.click(add);
    await waitFor(() => expect(syncInstances).toHaveBeenCalledWith("1", 1));
  });

  it("shows loading state while fetching servers", async () => {
    getInstances.mockResolvedValueOnce([]);
    let resolve;
    getPufferServers.mockReturnValueOnce(
      new Promise((res) => {
        resolve = res;
      }),
    );
    render(
      <MemoryRouter>
        <Instances />
      </MemoryRouter>,
    );
    const [addBtn] = await screen.findAllByRole("button", {
      name: /add instance/i,
    });
    fireEvent.click(addBtn);
    const toggle = screen.getByLabelText("Sync from PufferPanel");
    fireEvent.click(toggle);
    expect(toggle).toBeDisabled();
    screen.getByText(/loading/i);
    expect(screen.queryByLabelText("Server")).not.toBeInTheDocument();
    resolve([]);
    const select = await screen.findByLabelText("Server");
    expect(select).toBeInTheDocument();
    await waitFor(() => expect(toggle).not.toBeDisabled());
  });

  it("shows inline error with retry on server load failure", async () => {
    getInstances.mockResolvedValueOnce([]);
    getPufferServers
      .mockImplementationOnce(() => Promise.reject(new Error("boom")))
      .mockImplementationOnce(() =>
        Promise.resolve([{ id: "1", name: "One" }]),
      );
    render(
      <MemoryRouter>
        <Instances />
      </MemoryRouter>,
    );
    const [addBtn] = await screen.findAllByRole("button", {
      name: /add instance/i,
    });
    fireEvent.click(addBtn);
    fireEvent.click(screen.getByLabelText("Sync from PufferPanel"));
    await screen.findAllByRole("button", { name: /retry/i });
    expect(screen.getAllByText("boom")[0]).toBeInTheDocument();
  });

  it("shows scanning progress during sync", async () => {
    getInstances.mockResolvedValueOnce([]);
    getSecretStatus.mockResolvedValueOnce({
      exists: true,
      last4: "1234",
      updated_at: "",
    });
    getPufferServers.mockResolvedValueOnce([{ id: "1", name: "One" }]);
    addInstance.mockResolvedValueOnce({
      id: 1,
      name: "",
      loader: "",
      enforce_same_loader: true,
      pufferpanel_server_id: "1",
      mod_count: 0,
    });
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
    const [addBtn] = await screen.findAllByRole("button", {
      name: /add instance/i,
    });
    fireEvent.click(addBtn);
    fireEvent.click(screen.getByLabelText("Sync from PufferPanel"));
    const select = await screen.findByLabelText("Server");
    fireEvent.change(select, { target: { value: "1" } });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    expect(screen.getByText(/scanning/i)).toBeInTheDocument();
    await waitFor(() => expect(syncInstances).toHaveBeenCalledWith("1", 1));
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

  it("has no critical axe violations", async () => {
    getInstances.mockResolvedValueOnce([]);
    const { container } = render(
      <MemoryRouter>
        <Instances />
      </MemoryRouter>,
    );
    await screen.findAllByText("No instances");
    const results = await axe(container);
    const critical = results.violations.filter((v) => v.impact === "critical");
    expect(critical).toHaveLength(0);
  });
});
