import { render, waitFor } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { describe, it, expect } from "vitest";
import { toast } from "sonner";
import Toaster from "./Toaster.jsx";

describe("Toaster", () => {
  it("renders container once", async () => {
    render(<Toaster />);
    toast("hi");
    await waitFor(() =>
      expect(
        document.querySelectorAll('section[aria-label="Notifications alt+T"]'),
      ).toHaveLength(1),
    );
    const el = document
      .querySelector('section[aria-label="Notifications alt+T"]')
      .parentElement;
    expect(el).toMatchSnapshot();
  });
});
