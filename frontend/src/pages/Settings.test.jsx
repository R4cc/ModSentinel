import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi } from "vitest";

vi.mock("@/lib/api.ts", () => ({
  getSecretStatus: vi
    .fn()
    .mockResolvedValue({ exists: false, last4: "", updated_at: "" }),
  saveSecret: vi.fn(),
  clearSecret: vi.fn(),
  testPuffer: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("focus-trap-react", () => ({
  default: ({ children }) => children,
}));

import { MemoryRouter } from "react-router-dom";
import Settings from "./Settings.jsx";
import { saveSecret, clearSecret, testPuffer } from "@/lib/api.ts";
import { toast } from "sonner";

Object.defineProperty(window, "matchMedia", {
  writable: true,
  value: vi.fn().mockImplementation(() => ({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  })),
});

describe("Settings page", () => {
  it("saves and clears PufferPanel credentials", async () => {
    const { getSecretStatus } = await import("@/lib/api.ts");
    getSecretStatus
      .mockResolvedValueOnce({ exists: false, last4: "", updated_at: "" }) // modrinth
      .mockResolvedValueOnce({ exists: false, last4: "", updated_at: "" }) // puffer initial
      .mockResolvedValueOnce({ exists: true, last4: "1234", updated_at: "" }); // after save
    render(
      <MemoryRouter>
        <Settings />
      </MemoryRouter>,
    );
    expect(screen.getByText(/Requires scopes/)).toBeInTheDocument();
    expect(screen.getByText("server.view")).toBeInTheDocument();
    expect(screen.getByText("server.files.view")).toBeInTheDocument();
    expect(screen.getByText("server.files.edit")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Base URL"), {
      target: { value: "http://example.com" },
    });
    fireEvent.change(screen.getByLabelText("Client ID"), {
      target: { value: "id" },
    });
    fireEvent.change(screen.getByLabelText("Client Secret"), {
      target: { value: "secret" },
    });
    fireEvent.click(screen.getByLabelText("Enable deep scan"));
    const saveBtn = screen.getAllByRole("button", { name: "Save" })[1];
    fireEvent.click(saveBtn);
    await waitFor(() => expect(saveSecret).toHaveBeenCalled());
    expect(saveSecret).toHaveBeenCalledWith("pufferpanel", {
      base_url: "http://example.com",
      client_id: "id",
      client_secret: "secret",
      scopes: "server.view server.files.view server.files.edit",
      deep_scan: true,
    });

    const clearBtn = screen.getAllByRole("button", {
      name: "Revoke & Clear",
    })[1];
    fireEvent.click(clearBtn);
    await waitFor(() =>
      expect(clearSecret).toHaveBeenCalledWith("pufferpanel"),
    );
    expect(toast.success).toHaveBeenCalledWith("Credentials cleared");
  });

  it("tests PufferPanel connection", async () => {
    const { getSecretStatus } = await import("@/lib/api.ts");
    getSecretStatus
      .mockResolvedValueOnce({ exists: false, last4: "", updated_at: "" }) // modrinth
      .mockResolvedValueOnce({ exists: false, last4: "", updated_at: "" }); // puffer
    render(
      <MemoryRouter>
        <Settings />
      </MemoryRouter>,
    );
    fireEvent.change(screen.getByLabelText("Base URL"), {
      target: { value: "http://example.com" },
    });
    fireEvent.change(screen.getByLabelText("Client ID"), {
      target: { value: "id" },
    });
    fireEvent.change(screen.getByLabelText("Client Secret"), {
      target: { value: "secret" },
    });
    const btn = screen.getAllByRole("button", { name: /test connection/i })[0];
    fireEvent.click(btn);
    await waitFor(() =>
      expect(testPuffer).toHaveBeenCalledWith({
        base_url: "http://example.com",
        client_id: "id",
        client_secret: "secret",
        scopes: "server.view server.files.view server.files.edit",
      }),
    );
    expect(toast.success).toHaveBeenCalledWith("Connection ok");
  });
});
