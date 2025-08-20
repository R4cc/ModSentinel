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
} from "@/lib/api.ts";
import { toast } from "sonner";

describe("Instances page", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("shows empty state when no instances", async () => {
    getInstances.mockResolvedValueOnce([]);
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
    expect(
      await screen.findByText("Failed to load instances"),
    ).toBeInTheDocument();
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
      enforce_same_loader: false,
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
    fireEvent.click(screen.getByLabelText("Enforce same loader for mods"));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(updateInstance).toHaveBeenCalledWith(1, {
        name: "One!",
        loader: "fabric",
        enforce_same_loader: false,
      }),
    );
    expect(toast.success).toHaveBeenCalledWith("Instance updated");
  });
});
