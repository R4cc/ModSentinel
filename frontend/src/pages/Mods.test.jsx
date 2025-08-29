import {
  render,
  screen,
  fireEvent,
  waitFor,
  act,
  cleanup,
} from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

vi.mock("focus-trap-react", () => ({
  default: ({ children }) => children,
}));

vi.mock("@/lib/api.ts", () => ({
  getMods: vi.fn(),
  refreshMod: vi.fn(),
  deleteMod: vi.fn(),
  getInstance: vi.fn(),
  updateInstance: vi.fn(),
  instances: { sync: vi.fn() },
  jobs: { retry: vi.fn() },
  getSecretStatus: vi.fn(),
  checkMod: vi.fn(),
}));

vi.mock("@/lib/toast.ts", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

import { toast } from "@/lib/toast.ts";
import { axe } from "vitest-axe";

const confirmMock = vi.fn();
vi.mock("@/hooks/useConfirm.jsx", () => ({
  useConfirm: () => ({ confirm: confirmMock, ConfirmModal: null }),
}));
import Mods from "./Mods.jsx";

afterEach(() => {
  cleanup();
  eventSources.length = 0;
});
import {
  MemoryRouter,
  Route,
  Routes,
  createMemoryRouter,
  RouterProvider,
} from "react-router-dom";
import {
  getMods,
  getInstance,
  updateInstance,
  refreshMod,
  deleteMod,
  instances,
  jobs,
  getSecretStatus,
  checkMod,
} from "@/lib/api.ts";

const eventSources = [];
class MockEventSource {
  constructor(url) {
    this.url = url;
    this.onmessage = null;
    this.closed = false;
    eventSources.push(this);
  }
  close() {
    this.closed = true;
  }
}
global.EventSource = MockEventSource;

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/instances/1"]}>
      <Routes>
        <Route path="/instances/:id" element={<Mods />} />
      </Routes>
    </MemoryRouter>,
  );
}

function renderWithState(state) {
  return render(
    <MemoryRouter initialEntries={[{ pathname: "/instances/1", state }]}>
      <Routes>
        <Route path="/instances/:id" element={<Mods />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("Mods instance editing", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getSecretStatus.mockResolvedValue({
      exists: true,
      last4: "",
      updated_at: "",
    });
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

    fireEvent.click(screen.getByRole("button", { name: "Edit Name" }));
    const input = screen.getByDisplayValue("Old");
    fireEvent.change(input, { target: { value: "New" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(updateInstance).toHaveBeenCalledWith(1, {
        name: "New",
      }),
    );
    const heading = screen.getByRole("heading", { name: /New/ });
    expect(heading).toHaveTextContent("New");
    expect(heading).toHaveTextContent("fabric");

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    const checkbox = screen.getByLabelText("Enforce same loader for mods");
    fireEvent.click(checkbox);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(updateInstance).toHaveBeenLastCalledWith(1, {
        enforce_same_loader: false,
      }),
    );
  });
});

describe("Mods instance scoping", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getSecretStatus.mockResolvedValue({
      exists: true,
      last4: "",
      updated_at: "",
    });
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
    fireEvent.click(await screen.findByLabelText("Check for updates"));
    await waitFor(() => {
      expect(refreshMod).toHaveBeenCalledWith(
        1,
        expect.objectContaining({ instance_id: 1 }),
      );
      expect(toast.success).toHaveBeenCalledWith("Mod is up to date", {
        id: "mod-uptodate-1",
      });
      expect(toast.success).toHaveBeenCalledTimes(1);
    });
    fireEvent.click(screen.getAllByLabelText("Delete mod")[0]);
    await waitFor(() => expect(deleteMod).toHaveBeenCalledWith(1, 1));
  });

  it("renders project link for Modrinth mods", async () => {
    const modA = {
      id: 1,
      name: "Alpha",
      url: "https://example.com/a",
      icon_url: "https://cdn.modrinth.com/data/AANobbMI/icon.png",
      download_url:
        "https://cdn.modrinth.com/data/AANobbMI/versions/1.0.0/alpha.jar",
      game_version: "1.20",
      loader: "fabric",
      current_version: "1.0",
      available_version: "1.0",
      channel: "release",
      instance_id: 1,
    };
    getMods.mockResolvedValueOnce([modA]);
    renderPage();
    const link = await screen.findByRole("link", {
      name: "Open project page",
    });
    expect(link).toHaveAttribute("href", "https://modrinth.com/mod/AANobbMI");
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener");
  });

  it("disables project link for non-Modrinth mods", async () => {
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
    getMods.mockResolvedValueOnce([modA]);
    renderPage();
    const els = await screen.findAllByLabelText("Open project page");
    const btn = els.find((e) => e.tagName === "BUTTON");
    expect(btn).toBeDefined();
    expect(btn).toBeDisabled();
    expect(btn).not.toHaveAttribute("href");
    const row = btn.closest("tr");
    expect(row.querySelector('a[aria-label="Open project page"]')).toBeNull();
  });

  it("shows tooltips on hover and focus", async () => {
    renderPage();
    const refresh = await screen.findByLabelText("Check for updates");
    expect(
      screen.queryByRole("tooltip", { name: "Check for updates" }),
    ).toBeNull();
    fireEvent.focus(refresh);
    expect(
      screen.getByRole("tooltip", { name: "Check for updates" }),
    ).toBeInTheDocument();
    fireEvent.blur(refresh);
    await waitFor(() =>
      expect(
        screen.queryByRole("tooltip", { name: "Check for updates" }),
      ).toBeNull(),
    );
    fireEvent.mouseEnter(refresh);
    expect(
      screen.getByRole("tooltip", { name: "Check for updates" }),
    ).toBeInTheDocument();
  });

  it("has no critical accessibility issues", async () => {
    const { container } = renderPage();
    await screen.findByLabelText("Check for updates");
    const results = await axe(container, {
      rules: { "color-contrast": { enabled: false } },
    });
    expect(
      results.violations.filter((v) => v.impact === "critical"),
    ).toHaveLength(0);
  });

  it("navigates back to instances", async () => {
    const router = createMemoryRouter(
      [
        { path: "/instances/:id", element: <Mods /> },
        { path: "/instances", element: <div>Instances</div> },
      ],
      { initialEntries: ["/instances/1"] },
    );

    render(<RouterProvider router={router} />);

    await screen.findAllByRole("heading", { name: /Inst/ });
    const links = screen.getAllByRole("link", { name: /Back to Instances/ });
    fireEvent.click(links[links.length - 1]);
    await waitFor(() =>
      expect(router.state.location.pathname).toBe("/instances"),
    );
  });
});

describe("Mods unmatched files", () => {
  it("shows unmatched files with resolve", async () => {
    getMods.mockResolvedValue([]);
    getSecretStatus.mockResolvedValue({
      exists: true,
      last4: "",
      updated_at: "",
    });
    getInstance.mockResolvedValue({
      id: 1,
      name: "Srv",
      loader: "fabric",
      enforce_same_loader: true,
      created_at: "",
      mod_count: 0,
    });
    renderWithState({ unmatched: ["a.jar"] });
    expect(await screen.findByText("Unmatched files")).toBeInTheDocument();
    expect(screen.getByText("a.jar")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Resolve" })).toBeInTheDocument();
  });
});

describe("Mods from sync", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });
  it("uses mods from navigation state", async () => {
    getMods.mockResolvedValue([]);
    getSecretStatus.mockResolvedValue({
      exists: true,
      last4: "",
      updated_at: "",
    });
    const mod = {
      id: 1,
      name: "Sodium",
      url: "https://example.com/sodium",
      game_version: "1.20",
      loader: "fabric",
      current_version: "1.0",
      available_version: "1.0",
      channel: "release",
      available_channel: "release",
      download_url: "",
      icon_url: "",
      instance_id: 1,
    };
    getInstance.mockResolvedValue({
      id: 1,
      name: "Srv",
      loader: "fabric",
      enforce_same_loader: true,
      created_at: "",
      mod_count: 1,
    });
    renderWithState({ mods: [mod] });
    expect(await screen.findByText("Sodium")).toBeInTheDocument();
    expect(screen.getAllByText("1.0").length).toBeGreaterThan(0);
    expect(getMods).not.toHaveBeenCalled();
  });
});

describe("Mods resync", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getSecretStatus.mockResolvedValue({
      exists: true,
      last4: "",
      updated_at: "",
    });
    getInstance.mockResolvedValue({
      id: 1,
      name: "Srv",
      loader: "fabric",
      enforce_same_loader: true,
      created_at: "",
      mod_count: 0,
      pufferpanel_server_id: "srv1",
    });
    getMods.mockResolvedValue([]);
    instances.sync.mockResolvedValue({ id: 1, status: "queued" });
    jobs.retry.mockResolvedValue({ id: 1, status: "queued" });
  });

  it("shows progress and failure details with retry", async () => {
    renderPage();
    const btn = await screen.findByRole("button", { name: "Resync" });
    fireEvent.click(btn);
    await waitFor(() => expect(instances.sync).toHaveBeenCalledWith(1));
    expect(eventSources.length).toBe(1);
    act(() => {
      eventSources[0].onmessage({
        data: JSON.stringify({
          id: 1,
          status: "running",
          total: 10,
          processed: 5,
          succeeded: 4,
          failed: 1,
          in_queue: 5,
          failures: [{ name: "modA", error: "boom" }],
        }),
      });
    });
    expect(
      screen.getByText("5/10 processed (4 succeeded, 1 failed)"),
    ).toBeInTheDocument();
    expect(screen.getByText("modA")).toBeInTheDocument();
    act(() => {
      eventSources[0].onmessage({
        data: JSON.stringify({
          id: 1,
          status: "failed",
          total: 10,
          processed: 10,
          succeeded: 9,
          failed: 1,
          in_queue: 0,
          failures: [{ name: "modA", error: "boom" }],
        }),
      });
    });
    await waitFor(() => expect(eventSources[0].closed).toBe(true));
    const retryBtn = screen.getByRole("button", { name: "Retry failed" });
    fireEvent.click(retryBtn);
    await waitFor(() => expect(jobs.retry).toHaveBeenCalledWith(1));
  });
});

describe("Mods instance switching", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getSecretStatus.mockResolvedValue({
      exists: true,
      last4: "",
      updated_at: "",
    });
  });

  it("clears stale data when navigating between instances", async () => {
    const mod1 = {
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
    const mod2 = {
      id: 2,
      name: "Beta",
      url: "https://example.com/b",
      game_version: "1.20",
      loader: "forge",
      current_version: "1.0",
      available_version: "1.0",
      channel: "release",
      instance_id: 2,
    };
    const inst1 = {
      id: 1,
      name: "One",
      loader: "fabric",
      enforce_same_loader: true,
      created_at: "",
      mod_count: 1,
    };
    const inst2 = {
      id: 2,
      name: "Two",
      loader: "forge",
      enforce_same_loader: true,
      created_at: "",
      mod_count: 1,
    };

    getMods.mockImplementation((id) => {
      if (id === 1) return Promise.resolve([mod1]);
      if (id === 2) return Promise.resolve([mod2]);
    });

    getInstance.mockImplementation((id) => {
      if (id === 1) return Promise.resolve(inst1);
      if (id === 2) return Promise.resolve(inst2);
    });

    const router = createMemoryRouter(
      [{ path: "/instances/:id", element: <Mods /> }],
      { initialEntries: ["/instances/1"] },
    );

    const { container, findAllByText, findByText } = render(
      <RouterProvider router={router} />,
    );

    const alpha = await findAllByText("Alpha");
    expect(alpha[0]).toBeInTheDocument();

    await act(async () => {
      await router.navigate("/instances/2");
    });

    await waitFor(() => expect(getMods).toHaveBeenCalledWith(2));
    expect(await findByText("Beta")).toBeInTheDocument();
    const rows = container.querySelectorAll("tbody tr");
    expect(rows).toHaveLength(1);
    expect(rows[0]).toHaveTextContent("Beta");
  });
});

describe("Mods warnings", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getMods.mockResolvedValue([]);
    getInstance.mockResolvedValue({
      id: 1,
      name: "Srv",
      loader: "fabric",
      enforce_same_loader: true,
      created_at: "",
      mod_count: 0,
    });
    getSecretStatus.mockResolvedValue({
      exists: true,
      last4: "",
      updated_at: "",
    });
  });

  it("shows token missing warning", async () => {
    getSecretStatus.mockResolvedValue({
      exists: false,
      last4: "",
      updated_at: "",
    });
    renderPage();
    expect(
      await screen.findByText(
        "Set a Modrinth token in Settings to enable update checks.",
      ),
    ).toBeInTheDocument();
  });

  it("shows unsynced PufferPanel warning", async () => {
    getSecretStatus.mockResolvedValue({
      exists: true,
      last4: "",
      updated_at: "",
    });
    getInstance.mockResolvedValueOnce({
      id: 1,
      name: "Srv",
      loader: "fabric",
      enforce_same_loader: true,
      created_at: "",
      mod_count: 0,
      pufferpanel_server_id: "srv1",
    });
    renderPage();
    expect(
      await screen.findByText(
        "Instance has never been synced from PufferPanel.",
      ),
    ).toBeInTheDocument();
  });
});

describe("Mods rate limit", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getInstance.mockResolvedValue({
      id: 1,
      name: "Srv",
      loader: "fabric",
      enforce_same_loader: true,
      created_at: "",
      mod_count: 0,
    });
    getSecretStatus.mockResolvedValue({
      exists: true,
      last4: "",
      updated_at: "",
    });
  });

  it("shows rate limit banner", async () => {
    getMods.mockRejectedValueOnce(new Error("rate limited"));
    renderPage();
    expect(
      await screen.findByText(
        "Rate limit hit. Some requests are temporarily blocked.",
      ),
    ).toBeInTheDocument();
  });
});

describe("Mod icons", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getSecretStatus.mockResolvedValue({
      exists: true,
      last4: "",
      updated_at: "",
    });
    getInstance.mockResolvedValue({
      id: 1,
      name: "Srv",
      loader: "fabric",
      enforce_same_loader: true,
      created_at: "",
      mod_count: 1,
    });
  });

  it("renders icon when URL provided", async () => {
    const mod = {
      id: 1,
      name: "Alpha",
      url: "https://example.com/a",
      icon_url: "https://cdn.modrinth.com/data/AANobbMI/icon.png",
      game_version: "1.20",
      loader: "fabric",
      current_version: "1.0",
      available_version: "1.0",
      channel: "release",
      download_url: "",
      instance_id: 1,
    };
    getMods.mockResolvedValueOnce([mod]);
    renderPage();
    await screen.findAllByText("Alpha");
    const img = document.querySelector("tbody img");
    expect(img).not.toBeNull();
    expect(img.getAttribute("src")).toContain(mod.icon_url);
    expect(img).toHaveAttribute("loading", "lazy");
  });

  it("shows placeholder when icon missing", async () => {
    const mod = {
      id: 1,
      name: "Alpha",
      url: "https://example.com/a",
      icon_url: "",
      game_version: "1.20",
      loader: "fabric",
      current_version: "1.0",
      available_version: "1.0",
      channel: "release",
      download_url: "",
      instance_id: 1,
    };
    getMods.mockResolvedValueOnce([mod]);
    renderPage();
    await screen.findAllByText("Alpha");
    const tdPlaceholder = document.querySelector(
      'tbody [data-testid="icon-placeholder"]',
    );
    expect(document.querySelector("tbody img")).toBeNull();
    expect(tdPlaceholder).not.toBeNull();
  });

  it("shows placeholder when icon URL invalid", async () => {
    const mod = {
      id: 1,
      name: "Alpha",
      url: "https://example.com/a",
      icon_url: "http://",
      game_version: "1.20",
      loader: "fabric",
      current_version: "1.0",
      available_version: "1.0",
      channel: "release",
      download_url: "",
      instance_id: 1,
    };
    getMods.mockResolvedValueOnce([mod]);
    renderPage();
    await screen.findAllByText("Alpha");
    expect(document.querySelector("tbody img")).toBeNull();
    expect(
      document.querySelector('tbody [data-testid="icon-placeholder"]'),
    ).not.toBeNull();
  });

  it("falls back to placeholder on error", async () => {
    const mod = {
      id: 1,
      name: "Alpha",
      url: "https://example.com/a",
      icon_url: "https://cdn.modrinth.com/data/AANobbMI/icon.png",
      game_version: "1.20",
      loader: "fabric",
      current_version: "1.0",
      available_version: "1.0",
      channel: "release",
      download_url: "",
      instance_id: 1,
    };
    getMods.mockResolvedValueOnce([mod]);
    renderPage();
    await screen.findAllByText("Alpha");
    const img = document.querySelector("tbody img");
    fireEvent.error(img);
    await waitFor(() => {
      expect(
        document.querySelector('tbody [data-testid="icon-placeholder"]'),
      ).not.toBeNull();
    });
  });
});

describe("Check for updates", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getSecretStatus.mockResolvedValue({
      exists: true,
      last4: "",
      updated_at: "",
    });
    getInstance.mockResolvedValue({
      id: 1,
      name: "Inst",
      loader: "fabric",
      enforce_same_loader: true,
      created_at: "",
      mod_count: 3,
    });
  });

  it("scans mods and shows summary", async () => {
    const mods = [
      {
        id: 1,
        name: "Alpha",
        url: "a",
        icon_url: "",
        game_version: "1.20",
        loader: "fabric",
        current_version: "1.0",
        available_version: "1.0",
        channel: "release",
        download_url: "",
        instance_id: 1,
      },
      {
        id: 2,
        name: "Beta",
        url: "b",
        icon_url: "",
        game_version: "1.20",
        loader: "fabric",
        current_version: "1.0",
        available_version: "1.0",
        channel: "release",
        download_url: "",
        instance_id: 1,
      },
      {
        id: 3,
        name: "Gamma",
        url: "c",
        icon_url: "",
        game_version: "1.20",
        loader: "fabric",
        current_version: "1.0",
        available_version: "1.0",
        channel: "release",
        download_url: "",
        instance_id: 1,
      },
    ];
    getMods.mockResolvedValueOnce(mods);
    checkMod.mockImplementation((id) => {
      if (id === 1) return Promise.resolve(mods[0]);
      if (id === 2)
        return Promise.resolve({ ...mods[1], available_version: "2.0" });
      if (id === 3) return Promise.reject(new Error("boom"));
    });
    renderPage();
    await screen.findByText("Alpha");
    fireEvent.click(screen.getByRole("button", { name: "Check for Updates" }));
    await waitFor(() => expect(checkMod).toHaveBeenCalledTimes(3));
    expect(refreshMod).not.toHaveBeenCalled();
    await waitFor(() =>
      expect(
        screen.getByText("Up to date: 1, Outdated: 1, Errors: 1"),
      ).toBeInTheDocument(),
    );
    expect(screen.getByText("Alpha: Up to date")).toBeInTheDocument();
    expect(screen.getByText("Beta: Update 2.0 available")).toBeInTheDocument();
    expect(screen.getByText("Gamma: Error: boom")).toBeInTheDocument();
  });
});
