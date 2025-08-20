import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi } from "vitest";

vi.mock("@/lib/api.ts", () => ({
  getToken: vi.fn().mockResolvedValue(""),
  saveToken: vi.fn(),
  clearToken: vi.fn(),
  getPufferCreds: vi
    .fn()
    .mockResolvedValue({ base_url: "", client_id: "", client_secret: "" }),
  savePufferCreds: vi.fn(),
  clearPufferCreds: vi.fn(),
  testPufferCreds: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("focus-trap-react", () => ({
  default: ({ children }) => children,
}));

import { MemoryRouter } from "react-router-dom";
import Settings from "./Settings.jsx";
import {
  savePufferCreds,
  clearPufferCreds,
  testPufferCreds,
} from "@/lib/api.ts";
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
  it("saves, clears and tests PufferPanel credentials", async () => {
    render(
      <MemoryRouter>
        <Settings />
      </MemoryRouter>,
    );
    expect(screen.getByText(/Requires scopes/)).toBeInTheDocument();
    expect(screen.getByText("server.view")).toBeInTheDocument();
    expect(screen.getByText("server.files.view")).toBeInTheDocument();
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
    expect(savePufferCreds).toHaveBeenCalledWith({
      base_url: "http://example.com",
      client_id: "id",
      client_secret: "secret",
      deep_scan: true,
    });

    const testBtn = screen.getByRole("button", { name: "Test" });
    fireEvent.click(testBtn);
    expect(testPufferCreds).toHaveBeenCalledWith({
      base_url: "http://example.com",
      client_id: "id",
      client_secret: "secret",
      deep_scan: true,
    });

    const clearBtn = screen.getAllByRole("button", { name: "Clear" })[1];
    fireEvent.click(clearBtn);
    await waitFor(() => expect(clearPufferCreds).toHaveBeenCalled());
    expect(toast.success).toHaveBeenCalledWith("Credentials cleared");
  });
});
