import React from "react";
import { render, screen } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { describe, it, expect } from "vitest";
import InstanceStatusOverview from "@/components/InstanceStatusOverview.tsx";

describe("InstanceStatusOverview", () => {
  it("renders numbers and labels correctly", () => {
    render(
      <InstanceStatusOverview upToDate={10} updatesAvailable={3} failed={1} />,
    );

    // Numbers
    expect(screen.getByText("10")).toBeInTheDocument();
    expect(screen.getByText("3")).toBeInTheDocument();
    expect(screen.getByText("1")).toBeInTheDocument();

    // Default labels
    expect(screen.getByText(/Up to date/i)).toBeInTheDocument();
    expect(screen.getByText(/Updates available/i)).toBeInTheDocument();
    expect(screen.getByText(/Failed/i)).toBeInTheDocument();
  });

  it("matches snapshot layout", () => {
    const { container } = render(
      <InstanceStatusOverview upToDate={7} updatesAvailable={2} failed={0} />,
    );
    expect(container.firstChild).toMatchSnapshot();
  });
});

