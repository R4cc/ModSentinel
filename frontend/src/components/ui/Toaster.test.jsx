import { render, waitFor } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi } from "vitest";
import Toaster from "./Toaster.jsx";

const { toast } = await vi.importActual("@/lib/toast.ts");

describe("Toaster", () => {
  it("renders container once", async () => {
    render(<Toaster />);
    toast.info("hi");
    await waitFor(() =>
      expect(
        document.querySelectorAll('section[aria-label="Notifications alt+T"]'),
      ).toHaveLength(1),
    );
    const el = document.querySelector(
      'section[aria-label="Notifications alt+T"]',
    );
    expect(el).toBeInTheDocument();
  });
});
