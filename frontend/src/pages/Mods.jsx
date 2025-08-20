import { useEffect, useState } from "react";
import { useSearchParams, useParams, useLocation } from "react-router-dom";
import { Package, RefreshCw, Trash2, Plus, Pencil } from "lucide-react";
import { Input } from "@/components/ui/Input.jsx";
import { Select } from "@/components/ui/Select.jsx";
import { Button } from "@/components/ui/Button.jsx";
import { Modal } from "@/components/ui/Modal.jsx";
import { Checkbox } from "@/components/ui/Checkbox.jsx";
import {
  Table,
  TableHeader,
  TableRow,
  TableHead,
  TableBody,
  TableCell,
} from "@/components/ui/Table.jsx";
import { Skeleton } from "@/components/ui/Skeleton.jsx";
import { EmptyState } from "@/components/ui/EmptyState.jsx";
import {
  getMods,
  refreshMod,
  deleteMod,
  getToken,
  getInstance,
  updateInstance,
  resyncInstance,
} from "@/lib/api.ts";
import { cn } from "@/lib/utils.js";
import { toast } from "sonner";
import { useConfirm } from "@/hooks/useConfirm.jsx";
import { useOpenAddMod } from "@/hooks/useOpenAddMod.js";

export default function Mods() {
  const { id } = useParams();
  const instanceId = Number(id);
  const location = useLocation();
  const [mods, setMods] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [filter, setFilter] = useState("");
  const [searchParams] = useSearchParams();
  const status = searchParams.get("status") || "all";
  const [sort, setSort] = useState("name-asc");
  const [page, setPage] = useState(1);
  const perPage = 10;
  const { confirm, ConfirmModal } = useConfirm();
  const [hasToken, setHasToken] = useState(true);
  const openAddMod = useOpenAddMod(instanceId);
  const [instance, setInstance] = useState(null);
  const [editingName, setEditingName] = useState(false);
  const [name, setName] = useState("");
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [enforce, setEnforce] = useState(true);
  const [unmatched, setUnmatched] = useState([]);
  const [resyncing, setResyncing] = useState(false);

  useEffect(() => {
    getToken()
      .then((t) => setHasToken(!!t))
      .catch(() => setHasToken(false));
  }, []);

  useEffect(() => {
    fetchInstance();
    if (location.state?.mods) {
      setMods(location.state.mods);
      setLoading(false);
    } else {
      fetchMods();
    }
    if (location.state?.unmatched) {
      let next = location.state.unmatched;
      if (location.state?.resolved) {
        next = next.filter((f) => f !== location.state.resolved);
      }
      setUnmatched(next);
    } else if (location.state?.resolved) {
      setUnmatched((prev) => prev.filter((f) => f !== location.state.resolved));
    }
  }, [instanceId, location.state]);

  async function fetchMods() {
    setLoading(true);
    setError("");
    try {
      const data = await getMods(instanceId);
      setMods(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load mods");
    } finally {
      setLoading(false);
    }
  }

  async function fetchInstance() {
    try {
      const data = await getInstance(instanceId);
      setInstance(data);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load instance");
    }
  }

  async function handleResync() {
    if (!instance) return;
    setResyncing(true);
    try {
      const { instance: inst, unmatched: um, mods: m } = await resyncInstance(
        instance.id,
      );
      setInstance(inst);
      setUnmatched(um);
      setMods(m);
      toast.success("Resynced");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to resync");
    } finally {
      setResyncing(false);
    }
  }

  useEffect(() => {
    setPage(1);
  }, [filter, sort, status, instanceId]);

  const filtered = mods.filter((m) =>
    m.name.toLowerCase().includes(filter.toLowerCase()),
  );
  const statusFiltered = filtered.filter((m) => {
    if (status === "up_to_date")
      return m.current_version === m.available_version;
    if (status === "outdated") return m.current_version !== m.available_version;
    return true;
  });
  const sorted = [...statusFiltered].sort((a, b) =>
    sort === "name-desc"
      ? b.name.localeCompare(a.name)
      : a.name.localeCompare(b.name),
  );
  const totalPages = Math.ceil(sorted.length / perPage) || 1;
  const current = sorted.slice((page - 1) * perPage, page * perPage);

  async function handleCheck(m) {
    try {
      const data = await refreshMod(m.id, {
        url: m.url,
        loader: m.loader,
        game_version: m.game_version,
        channel: m.channel,
        instance_id: m.instance_id,
      });
      setMods(data);
      const updated = data.find((x) => x.id === m.id);
      if (updated && updated.available_version !== updated.current_version) {
        toast.info(`New version ${updated.available_version} available`);
      } else {
        toast.success("Mod is up to date");
      }
    } catch (err) {
      if (err instanceof Error && err.message === "token required") {
        toast.error("Modrinth token required");
      } else if (err instanceof Error) {
        toast.error(err.message);
      } else {
        toast.error("Failed to check updates");
      }
    }
  }

  async function handleDelete(id) {
    const ok = await confirm({
      title: "Delete mod",
      message: "Are you sure you want to delete this mod?",
      confirmText: "Delete",
    });
    if (!ok) return;
    try {
      const data = await deleteMod(id, instanceId);
      setMods(data);
      toast.success("Mod deleted");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to delete mod");
    }
  }

  async function saveName(e) {
    e.preventDefault();
    if (!name.trim()) {
      toast.error("Name required");
      return;
    }
    try {
      const updated = await updateInstance(instance.id, { name });
      setInstance(updated);
      toast.success("Instance updated");
      setEditingName(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to save instance");
    }
  }

  async function saveSettings(e) {
    e.preventDefault();
    try {
      const updated = await updateInstance(instance.id, {
        enforce_same_loader: enforce,
      });
      setInstance(updated);
      toast.success("Instance updated");
      setSettingsOpen(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to save instance");
    }
  }

  return (
    <div className="space-y-md">
      {ConfirmModal}
      {instance && (
        <div className="flex flex-wrap items-center gap-sm">
          {editingName ? (
            <form
              className="flex flex-wrap items-center gap-sm"
              onSubmit={saveName}
            >
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="h-7 w-48"
              />
              <Button size="sm" type="submit">
                Save
              </Button>
              <Button
                size="sm"
                type="button"
                variant="secondary"
                onClick={() => setEditingName(false)}
              >
                Cancel
              </Button>
            </form>
          ) : (
            <>
              <h1 className="flex items-center gap-xs text-2xl font-bold">
                {instance.name}
                <span className="text-lg font-normal text-muted-foreground capitalize">
                  ({instance.loader})
                </span>
              </h1>
              <Button
                variant="ghost"
                size="icon"
                onClick={() => {
                  setName(instance.name);
                  setEditingName(true);
                }}
                aria-label="Rename instance"
              >
                <Pencil className="h-4 w-4" />
              </Button>
            </>
          )}
          {editingName && (
            <span className="text-lg font-normal text-muted-foreground capitalize">
              ({instance.loader})
            </span>
          )}
          <Button
            size="sm"
            variant="secondary"
            onClick={() => {
              setEnforce(instance.enforce_same_loader);
              setSettingsOpen(true);
            }}
          >
            Edit
          </Button>
          {instance.pufferpanel_server_id && (
            <Button
              size="sm"
              variant="secondary"
              onClick={handleResync}
              disabled={resyncing}
            >
              {resyncing ? "Resyncing..." : "Resync from PufferPanel"}
            </Button>
          )}
        </div>
      )}
      {instance?.last_sync_at && (
        <p className="text-sm text-muted-foreground">
          Last sync: {new Date(instance.last_sync_at).toLocaleString()} (added
          {" "}
          {instance.last_sync_added}, changed {instance.last_sync_updated},
          failed {instance.last_sync_failed})
        </p>
      )}
      {unmatched.length > 0 && (
        <div>
          <h2 className="text-lg font-medium">Unmatched files</h2>
          <ul className="space-y-xs" data-testid="unmatched-files">
            {unmatched.map((f) => (
              <li
                key={f}
                className="flex items-center justify-between rounded border border-yellow-200 bg-yellow-50 px-sm py-xs text-sm text-yellow-800"
              >
                <span className="truncate" title={f}>
                  {f}
                </span>
                <Button size="sm" variant="outline" onClick={() => openAddMod(f)}>
                  Resolve
                </Button>
              </li>
            ))}
          </ul>
        </div>
      )}
      <div className="flex flex-col gap-sm sm:flex-row sm:items-center">
        <div className="flex flex-col">
          <label htmlFor="filter" className="sr-only">
            Filter mods
          </label>
          <Input
            id="filter"
            placeholder="Filter mods..."
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="sm:max-w-xs"
          />
        </div>
        <div className="flex flex-col">
          <label htmlFor="sort" className="sr-only">
            Sort mods
          </label>
          <Select
            id="sort"
            value={sort}
            onChange={(e) => setSort(e.target.value)}
            className="sm:w-40"
          >
            <option value="name-asc">Name A–Z</option>
            <option value="name-desc">Name Z–A</option>
          </Select>
        </div>
        <Button
          onClick={openAddMod}
          className={cn(
            "w-full gap-xs sm:ml-auto sm:w-auto",
            !hasToken && "pointer-events-none opacity-50",
          )}
          disabled={!hasToken}
          title="Add Mod"
        >
          <Plus className="h-4 w-4" aria-hidden="true" />
          Add Mod
        </Button>
      </div>

      {!hasToken && (
        <p className="text-sm text-muted-foreground">
          Set a Modrinth token in settings to enable update checks.
        </p>
      )}

      {loading && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Game</TableHead>
              <TableHead>Loader</TableHead>
              <TableHead>Current</TableHead>
              <TableHead>Available</TableHead>
              <TableHead>Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {Array.from({ length: 5 }).map((_, i) => (
              <TableRow key={i}>
                <TableCell>
                  <Skeleton className="h-4 w-32" />
                </TableCell>
                <TableCell>
                  <Skeleton className="h-4 w-20" />
                </TableCell>
                <TableCell>
                  <Skeleton className="h-4 w-16" />
                </TableCell>
                <TableCell>
                  <Skeleton className="h-4 w-20" />
                </TableCell>
                <TableCell>
                  <Skeleton className="h-4 w-20" />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      {!loading && error && (
        <div className="flex flex-col items-center gap-sm">
          <p className="text-sm text-muted-foreground">{error}</p>
          <Button onClick={fetchMods}>Retry</Button>
        </div>
      )}

      {!loading && !error && current.length === 0 && (
        <EmptyState
          icon={Package}
          title="No mods"
          message="You haven't added any mods yet."
        />
      )}

      {!loading && !error && current.length > 0 && (
        <>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Game</TableHead>
                <TableHead>Loader</TableHead>
                <TableHead>Current</TableHead>
                <TableHead>Available</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {current.map((m) => (
                <TableRow key={m.id}>
                  <TableCell className="flex items-center gap-sm font-medium">
                    {m.icon_url && (
                      <img
                        src={m.icon_url}
                        alt=""
                        className="h-6 w-6 rounded-sm"
                      />
                    )}
                    {m.name || m.url}
                  </TableCell>
                  <TableCell>{m.game_version}</TableCell>
                  <TableCell>{m.loader}</TableCell>
                  <TableCell>{m.current_version}</TableCell>
                  <TableCell>{m.available_version}</TableCell>
                  <TableCell className="flex gap-xs">
                    <Button
                      variant="outline"
                      onClick={() => handleCheck(m)}
                      aria-label="Check for updates"
                      className="h-8 px-sm"
                      disabled={!hasToken}
                    >
                      <RefreshCw className="h-4 w-4" aria-hidden="true" />
                    </Button>
                    <Button
                      variant="outline"
                      onClick={() => handleDelete(m.id)}
                      aria-label="Delete mod"
                      className="h-8 px-sm"
                    >
                      <Trash2 className="h-4 w-4" aria-hidden="true" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>

          <div className="flex items-center justify-between pt-sm">
            <Button
              variant="outline"
              onClick={() => setPage((p) => p - 1)}
              disabled={page === 1}
            >
              Previous
            </Button>
            <span className="text-sm text-muted-foreground">
              Page {page} of {totalPages}
            </span>
            <Button
              variant="outline"
              onClick={() => setPage((p) => p + 1)}
              disabled={page >= totalPages}
            >
              Next
            </Button>
          </div>
        </>
      )}
      <Modal open={settingsOpen} onClose={() => setSettingsOpen(false)}>
        <form className="space-y-md" onSubmit={saveSettings}>
          <div className="space-y-xs">
            <span className="text-sm font-medium">Loader</span>
            <p className="text-sm">{instance?.loader}</p>
          </div>
          <div className="flex items-center gap-sm">
            <Checkbox
              id="enforce"
              checked={enforce}
              onChange={(e) => setEnforce(e.target.checked)}
            />
            <label htmlFor="enforce" className="text-sm">
              Enforce same loader for mods
            </label>
          </div>
          <div className="flex justify-end gap-sm">
            <Button
              type="button"
              variant="secondary"
              onClick={() => setSettingsOpen(false)}
            >
              Cancel
            </Button>
            <Button type="submit">Save</Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
