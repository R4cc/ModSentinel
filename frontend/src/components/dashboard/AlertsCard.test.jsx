import { render, screen } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { describe, it, vi, beforeEach } from "vitest";
import { MemoryRouter } from "react-router-dom";

vi.mock("@/lib/api.ts", () => ({
  getSecretStatus: vi.fn(),
}));

import AlertsCard from "./AlertsCard.jsx";

beforeEach(() => {
  sessionStorage.clear();
});

describe("AlertsCard", () => {
  it("shows no alerts when token exists and no error", async () => {
    const { getSecretStatus } = await import("@/lib/api.ts");
    getSecretStatus.mockResolvedValue({
      exists: true,
      last4: "",
      updated_at: "",
    });
    render(
      <MemoryRouter>
        <AlertsCard error="" onRetry={() => {}} />
      </MemoryRouter>,
    );
    await screen.findByText("No alerts.");
  });

  it("shows token alert when token missing", async () => {
    const { getSecretStatus } = await import("@/lib/api.ts");
    getSecretStatus.mockResolvedValue({
      exists: false,
      last4: "",
      updated_at: "",
    });
    render(
      <MemoryRouter>
        <AlertsCard error="" onRetry={() => {}} />
      </MemoryRouter>,
    );
    await screen.findByText("Modrinth token required.");
  });

  it("shows rate limit alert", async () => {
    const { getSecretStatus } = await import("@/lib/api.ts");
    getSecretStatus.mockResolvedValue({
      exists: true,
      last4: "",
      updated_at: "",
    });
    render(
      <MemoryRouter>
        <AlertsCard error="rate limited" onRetry={() => {}} />
      </MemoryRouter>,
    );
    await screen.findByText("Rate limit hit.");
  });
});
