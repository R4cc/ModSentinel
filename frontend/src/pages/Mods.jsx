import { useEffect, useState } from "react";
import {
  useSearchParams,
  useParams,
  useLocation,
  Link,
} from "react-router-dom";
import {
  Package,
  RefreshCw,
  RotateCw,
  Trash2,
  Plus,
  ExternalLink,
  ArrowLeft,
  Pencil,
  Key,
  AlertTriangle,
} from "lucide-react";
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
import ModIcon from "@/components/ModIcon.jsx";
import { Tooltip } from "@/components/ui/Tooltip.jsx";
import { Badge } from "@/components/ui/Badge.jsx";
import {
  getMods,
  refreshMod,
  deleteMod,
  getInstance,
  getInstances,
  updateInstance,
  instances,
  jobs,
  getSecretStatus,
  checkMod,
} from "@/lib/api.ts";
import { cn } from "@/lib/utils.js";
import { toast } from "@/lib/toast.ts";
import { useConfirm } from "@/hooks/useConfirm.jsx";
import { useOpenAddMod } from "@/hooks/useOpenAddMod.js";

export default function Mods() {
  const { id } = useParams();
  const instanceId = Number(id);
  const location = useLocation();
  const [mods, setMods] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [rateLimited, setRateLimited] = useState(false);
  const [filter, setFilter] = useState("");
  const [searchParams] = useSearchParams();
  const status = searchParams.get("status") || "all";
  const [sort, setSort] = useState("name-asc");
  const [page, setPage] = useState(1);
  const perPage = 40;
  const { confirm, ConfirmModal } = useConfirm();
  const [hasToken, setHasToken] = useState(true);
  const openAddMod = useOpenAddMod(instanceId);
  const [instance, setInstance] = useState(null);
  const [nameSuffix, setNameSuffix] = useState(0);
  const [editOpen, setEditOpen] = useState(false);
  const [name, setName] = useState("");
  const [enforce, setEnforce] = useState(true);
  const [unmatched, setUnmatched] = useState([]);
  const [resyncing, setResyncing] = useState(false);
  const [progress, setProgress] = useState(null);
  const [source, setSource] = useState(null);
  const [checkOpen, setCheckOpen] = useState(false);
  const [checkingAll, setCheckingAll] = useState(false);
  const [checkProgress, setCheckProgress] = useState(0);
  const [checkResults, setCheckResults] = useState([]);
  const [checkSummary, setCheckSummary] = useState({
    updated: 0,
    outdated: 0,
    errors: 0,
  });

  // helper for colored loader badge
  function loaderBadgeClass(loader) {
    switch ((loader || "").toLowerCase()) {
      case "fabric":
        return "bg-emerald-100 text-emerald-800 border-emerald-200";
      case "forge":
        return "bg-orange-100 text-orange-800 border-orange-200";
      case "quilt":
        return "bg-purple-100 text-purple-800 border-purple-200";
      case "paper":
      case "spigot":
      case "bukkit":
        return "bg-sky-100 text-sky-800 border-sky-200";
      default:
        return "bg-muted text-foreground";
    }
  }

  useEffect(() => {
    getSecretStatus("modrinth")
      .then((s) => setHasToken(s.exists))
      .catch(() => setHasToken(false));
  }, []);

  useEffect(() => {
    setInstance(null);
    setMods([]);
    setUnmatched([]);
    setLoading(true);
    fetchInstance();
    // compute suffix against all instances for header disambiguation
    computeNameSuffix();
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

  async function computeNameSuffix() {
    try {
      const [current, all] = await Promise.all([
        getInstance(instanceId),
        getInstances(),
      ]);
      const same = all.filter((i) => i.name === current.name);
      if (same.length <= 1) {
        setNameSuffix(0);
        setInstance(current);
        return;
      }
      same.sort((a, b) => {
        const ta = Date.parse(a.created_at || "");
        const tb = Date.parse(b.created_at || "");
        if (!isNaN(ta) && !isNaN(tb) && ta !== tb) return ta - tb;
        return (a.id || 0) - (b.id || 0);
      });
      const idx = same.findIndex((i) => i.id === current.id);
      setNameSuffix(idx > 0 ? idx : 0);
      setInstance(current);
    } catch {
      // fall back to existing fetch flow if any error
    }
  }

  useEffect(() => {
    return () => {
      if (source) source.close();
    };
  }, [source]);

  async function fetchMods() {
    setLoading(true);
    setError("");
    try {
      const data = await getMods(instanceId);
      setMods(data);
      setRateLimited(false);
    } catch (err) {
      if (err instanceof Error && err.message === "rate limited") {
        setRateLimited(true);
      } else {
        setError(err instanceof Error ? err.message : "Failed to load mods");
      }
    } finally {
      setLoading(false);
    }
  }

  async function fetchInstance() {
    try {
      const data = await getInstance(instanceId);
      setInstance(data);
      setRateLimited(false);
    } catch (err) {
      if (err instanceof Error && err.message === "rate limited") {
        setRateLimited(true);
      } else {
        toast.error(
          err instanceof Error ? err.message : "Failed to load instance",
        );
      }
    }
  }

  async function handleResync() {
    if (!instance) return;
    setResyncing(true);
    setProgress(null);
    try {
      const { id } = await instances.sync(instance.id);
      trackJob(id);
    } catch (err) {
      if (err instanceof Error && err.message === "rate limited") {
        setRateLimited(true);
      }
      toast.error(err instanceof Error ? err.message : "Failed to resync");
      setResyncing(false);
    }
  }

  async function handleRetryFailed() {
    if (!progress) return;
    setResyncing(true);
    setProgress(null);
    try {
      await jobs.retry(progress.id);
      trackJob(progress.id);
    } catch (err) {
      if (err instanceof Error && err.message === "rate limited") {
        setRateLimited(true);
      }
      toast.error(err instanceof Error ? err.message : "Failed to retry");
      setResyncing(false);
    }
  }

  function trackJob(id) {
    const es = new EventSource(`/api/jobs/${id}/events`);
    setSource(es);
    es.onmessage = (e) => {
      const data = JSON.parse(e.data);
      setProgress(data);
      if (
        data.status === "succeeded" ||
        data.status === "failed" ||
        data.status === "canceled"
      ) {
        es.close();
        setSource(null);
        setResyncing(false);
        fetchInstance();
        fetchMods();
        // Surface unresolved files as virtual entries
        if (Array.isArray(data.failures) && data.failures.length > 0) {
          setUnmatched(Array.from(new Set(data.failures.map((f) => f.name).filter(Boolean))));
        } else {
          setUnmatched([]);
        }
        if (data.status === "succeeded") {
          toast.success("Resynced");
        } else {
          toast.error(
            data.status === "failed" ? "Sync failed" : "Sync canceled",
          );
        }
        if (data.failed === 0) {
          setProgress(null);
        }
      }
    };
  }

  async function handleCheckAll() {
    if (mods.length === 0) return;
    setCheckOpen(true);
    setCheckingAll(true);
    setCheckProgress(0);
    setCheckResults([]);
    setCheckSummary({ updated: 0, outdated: 0, errors: 0 });
    const limit = 3;
    let index = 0;
    async function worker() {
      while (index < mods.length) {
        const mod = mods[index++];
        try {
          const res = await checkMod(mod.id);
          const up = res.available_version === res.current_version;
          setCheckResults((prev) => [
            ...prev,
            {
              id: mod.id,
              name: mod.name || mod.url,
              status: up ? "updated" : "outdated",
              available_version: res.available_version,
            },
          ]);
          setCheckSummary((s) => ({
            ...s,
            updated: s.updated + (up ? 1 : 0),
            outdated: s.outdated + (up ? 0 : 1),
          }));
        } catch (err) {
          const msg = err instanceof Error ? err.message : "Failed to check";
          if (msg === "rate limited") {
            setRateLimited(true);
          }
          setCheckResults((prev) => [
            ...prev,
            {
              id: mod.id,
              name: mod.name || mod.url,
              status: "error",
              error: msg,
            },
          ]);
          setCheckSummary((s) => ({ ...s, errors: s.errors + 1 }));
        } finally {
          setCheckProgress((p) => p + 1);
        }
      }
    }
    await Promise.all(
      Array.from({ length: Math.min(limit, mods.length) }, () => worker()),
    );
    setCheckingAll(false);
  }

  useEffect(() => {
    setPage(1);
  }, [filter, sort, status, instanceId]);

  // Build virtual unresolved entries from progress (when finished) and from navigation state
  const unresolvedSet = new Set(unmatched);
  if (progress && progress.status !== "running" && Array.isArray(progress.failures)) {
    for (const f of progress.failures) if (f?.name) unresolvedSet.add(f.name);
  }
  const virtuals = Array.from(unresolvedSet).map((name, i) => ({
    id: -1000 - i,
    name,
    icon_url: "",
    url: "",
    game_version: "",
    loader: instance?.loader || "",
    channel: "",
    current_version: "",
    available_version: "",
    download_url: "",
    instance_id: instanceId,
    virtual: true,
    file: name,
  }));

  const withVirtuals = [...mods, ...virtuals];

  const filtered = withVirtuals.filter((m) =>
    m.name.toLowerCase().includes(filter.toLowerCase()),
  );
  const statusFiltered = filtered.filter((m) => {
    if (m.virtual) return status === "all"; // only show virtuals in 'all'
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
      if (!Array.isArray(data)) {
        throw new Error("Unexpected response");
      }
      setMods(data);
      const updated = data.find((x) => x.id === m.id);
      if (updated && updated.available_version !== updated.current_version) {
        toast.info(`New version ${updated.available_version} available`, {
          id: `mod-update-${m.id}`,
        });
      } else {
        toast.success("Mod is up to date", {
          id: `mod-uptodate-${m.id}`,
        });
      }
    } catch (err) {
      if (err instanceof Error && err.message === "token required") {
        toast.error("Modrinth token required");
      } else if (err instanceof Error && err.message === "rate limited") {
        setRateLimited(true);
        toast.error(err.message);
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

  async function saveSettings(e) {
    e.preventDefault();
    if (!name.trim()) {
      toast.error("Name required");
      return;
    }
    try {
      const updated = await updateInstance(instance.id, { name, enforce_same_loader: enforce });
      setInstance(updated);
      toast.success("Instance updated");
      setEditOpen(false);
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Failed to save instance",
      );
    }
  }

  function getProjectUrl(m) {
    const extractId = (url) => {
      if (!url) return null;
      const match = url.match(/cdn\.modrinth\.com\/data\/([^/]+)/);
      return match ? match[1] : null;
    };
    const extractSlug = (url) => {
      if (!url) return null;
      try {
        const u = new URL(url);
        if (!u.hostname.endsWith("modrinth.com")) return null;
        const parts = u.pathname.split("/").filter(Boolean);
        const idx = parts.findIndex((p) =>
          ["mod", "plugin", "datapack", "resourcepack"].includes(p),
        );
        if (idx !== -1 && idx + 1 < parts.length) return parts[idx + 1];
      } catch {
        return null;
      }
      return null;
    };
    const id = extractId(m.download_url) || extractId(m.icon_url);
    if (id) return `https://modrinth.com/mod/${id}`;
    const slug = extractSlug(m.url);
    if (slug) return `https://modrinth.com/mod/${slug}`;
    if (m.url?.includes("modrinth.com")) return m.url;
    return null;
  }

  return (
    <div className="space-y-md">
      {ConfirmModal}
      <Modal open={checkOpen} onClose={() => setCheckOpen(false)}>
        <h2 className="mb-sm text-lg font-medium">Check for Updates</h2>
        <p className="text-sm mb-sm">
          {checkProgress} / {mods.length} checked
        </p>
        <ul className="max-h-60 overflow-y-auto space-y-xs">
          {checkResults.map((r) => (
            <li key={r.id} className="text-sm">
              {r.name}:{" "}
              {r.status === "error"
                ? `Error: ${r.error}`
                : r.status === "outdated"
                  ? `Update ${r.available_version} available`
                  : "Up to date"}
            </li>
          ))}
        </ul>
        {checkProgress === mods.length && (
          <p className="mt-sm text-sm">
            Up to date: {checkSummary.updated}, Outdated:{" "}
            {checkSummary.outdated}, Errors: {checkSummary.errors}
          </p>
        )}
        <div className="mt-sm flex justify-end">
          <Button
            size="sm"
            onClick={() => setCheckOpen(false)}
            disabled={checkingAll}
          >
            Close
          </Button>
        </div>
      </Modal>
      {rateLimited && (
        <div
          className="rounded-md border border-amber-500 bg-amber-50 p-sm text-amber-900"
          role="status"
        >
          Rate limit hit. Some requests are temporarily blocked.
        </div>
      )}
      <Link
        to="/instances"
        className="inline-flex items-center gap-xs text-sm text-muted-foreground hover:underline"
      >
        <ArrowLeft className="h-4 w-4" aria-hidden="true" />
        Back to Instances
      </Link>
      {!hasToken && (
        <div
          className="mt-sm flex max-w-xl items-center gap-sm rounded-md border border-yellow-200 bg-yellow-50 p-sm text-yellow-800"
          role="status"
        >
          <Key className="h-4 w-4" aria-hidden />
          <span className="text-sm">
            Set a Modrinth token in Settings to enable update checks.
          </span>
        </div>
      )}
      {instance && (
        <div className="space-y-sm">
          <div className="flex items-center gap-sm">
            <h1 className="text-2xl font-bold truncate">{instance.name}{nameSuffix ? ` (${nameSuffix})` : ""}</h1>
            <Badge
              variant="secondary"
              className={`capitalize border ${loaderBadgeClass(instance.loader)}`}
            >
              {instance.loader || "unknown"}
            </Badge>
          </div>
          <div className="flex flex-wrap items-center gap-sm">
            <Button
              variant="outline"
              onClick={handleCheckAll}
              disabled={checkingAll || !hasToken || mods.length === 0}
              className="gap-xs"
            >
              <RefreshCw className="h-4 w-4" aria-hidden />
              Check Updates
            </Button>
            {instance.pufferpanel_server_id && (
              <Button
                variant="secondary"
                onClick={handleResync}
                disabled={resyncing}
                className="gap-xs"
              >
                <RotateCw className="h-4 w-4" aria-hidden />
                {resyncing ? "Resyncing" : "Resync"}
              </Button>
            )}
            <Button
              variant="outline"
              onClick={() => {
                setName(instance.name);
                setEnforce(instance.enforce_same_loader);
                setEditOpen(true);
              }}
              className="gap-xs"
            >
              <Pencil className="h-4 w-4" aria-hidden />
              Edit
            </Button>
          </div>
        </div>
      )}
      {/* Sync status / progress card */}
      {(progress || instance?.last_sync_at) && (
        <div className="rounded border p-sm space-y-xs w-full max-w-3xl">
          <div className="flex items-center justify-between gap-sm">
            <div className="flex items-center gap-sm">
              <RotateCw
                className={cn(
                  "h-4 w-4",
                  progress?.status === "running" ? "animate-spin" : "opacity-60",
                )}
                aria-hidden
              />
              <p className="text-sm font-medium">
                {progress?.status === "running"
                  ? "Syncing from PufferPanel"
                  : "Last sync summary"}
              </p>
            </div>
            {!progress && instance?.last_sync_at && (
              <span className="text-xs text-muted-foreground">
                {new Date(instance.last_sync_at).toLocaleString()}
              </span>
            )}
          </div>
          {/* Progress bar while running */}
          {progress?.status === "running" && (
            <>
              <div
                className="h-2 bg-muted rounded"
                role="progressbar"
                aria-valuemin={0}
                aria-valuemax={progress.total}
                aria-valuenow={progress.processed}
              >
                <div
                  className="h-2 bg-primary rounded transition-[width] duration-300"
                  style={{
                    width: progress.total
                      ? `${(progress.processed / progress.total) * 100}%`
                      : "0%",
                  }}
                />
              </div>
              <p className="text-sm">
                {progress.processed}/{progress.total} processed (
                {progress.succeeded} succeeded, {progress.failed} failed)
              </p>
            </>
          )}
          {/* Segmented result bar after done */}
          {progress && progress.status !== "running" && (
            <>
              {(() => {
                const total = progress.total || 0;
                const ok = progress.succeeded || 0;
                const fail = progress.failed || 0;
                const okPct = total ? (ok / total) * 100 : 0;
                const failPct = total ? (fail / total) * 100 : 0;
                return (
                  <div className="h-2 bg-muted rounded overflow-hidden">
                    <div
                      className="h-2 bg-emerald-500 inline-block"
                      style={{ width: `${okPct}%` }}
                    />
                    <div
                      className="h-2 bg-red-500 inline-block"
                      style={{ width: `${failPct}%` }}
                    />
                  </div>
                );
              })()}
              <div className="flex items-center justify-between">
                <p className="text-sm">
                  {progress.succeeded} succeeded, {progress.failed} failed
                </p>
                {progress.failed > 0 && (
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => setCheckOpen((v) => !v)}
                  >
                    {checkOpen ? "Hide failures" : "View failures"}
                  </Button>
                )}
              </div>
            </>
          )}
          {/* Last sync summary if no live progress and info available */}
          {!progress && instance?.last_sync_at && (
            <>
              {(() => {
                const ok = (instance.last_sync_added || 0) + (instance.last_sync_updated || 0);
                const fail = instance.last_sync_failed || 0;
                const total = ok + fail;
                const okPct = total ? (ok / total) * 100 : 0;
                const failPct = total ? (fail / total) * 100 : 0;
                return (
                  <div className="h-2 bg-muted rounded overflow-hidden">
                    <div
                      className="h-2 bg-emerald-500 inline-block"
                      style={{ width: `${okPct}%` }}
                    />
                    <div
                      className="h-2 bg-red-500 inline-block"
                      style={{ width: `${failPct}%` }}
                    />
                  </div>
                );
              })()}
              <div className="flex items-center justify-between">
                <p className="text-sm text-muted-foreground">
                  Added {instance.last_sync_added}, changed {instance.last_sync_updated}, failed {instance.last_sync_failed}
                </p>
              </div>
            </>
          )}
          {/* Failure details collapsible */}
          {progress && progress.failures && progress.failures.length > 0 && checkOpen && (
            <>
              <Table className="text-sm">
                <TableHeader>
                  <TableRow>
                    <TableHead>Mod</TableHead>
                    <TableHead>Error</TableHead>
                    <TableHead className="text-right"></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {progress.failures.map((f) => (
                    <TableRow key={f.name}>
                      <TableCell>{f.name}</TableCell>
                      <TableCell>{f.error}</TableCell>
                      <TableCell className="text-right">
                        <Button size="sm" onClick={handleResync}>
                          Retry
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              {progress.status !== "running" && (
                <div className="text-right">
                  <Button size="sm" onClick={handleRetryFailed} data-testid="retry-failed">
                    Retry failed
                  </Button>
                </div>
              )}
            </>
          )}
        </div>
      )}
      <div className="min-h-5 space-y-xs">
        {instance?.pufferpanel_server_id && !instance?.last_sync_at && (
          <p className="text-sm text-muted-foreground">
            Instance has never been synced from PufferPanel.
          </p>
        )}
      </div>
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
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => openAddMod(f)}
                >
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
                <TableHead>Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {current.map((m) => {
                const projectUrl = getProjectUrl(m);
                const isModrinth = !!projectUrl;
                return (
                  <TableRow key={m.id}>
                    <TableCell className="flex items-center gap-sm font-medium">
                      <ModIcon
                        url={m.icon_url}
                        cacheKey={
                          m.available_version ||
                          m.current_version ||
                          String(m.id)
                        }
                      />
                      <span className="flex items-center gap-xs">
                        {m.name || m.url}
                        {m.virtual && (
                          <span className="inline-flex items-center gap-1 text-xs text-red-600" role="status">
                            <AlertTriangle className="h-4 w-4" aria-hidden />
                            Could not be matched
                          </span>
                        )}
                      </span>
                    </TableCell>
                    <TableCell>{m.game_version}</TableCell>
                    <TableCell>{m.loader}</TableCell>
                    <TableCell>{m.current_version}</TableCell>
                    <TableCell>{m.available_version}</TableCell>
                    <TableCell className="flex gap-xs">
                      {m.virtual ? (
                        <Button
                          variant="outline"
                          onClick={() => openAddMod(m.file || m.name)}
                          aria-label="Match mod"
                          className="h-8 px-sm"
                        >
                          Match mod
                        </Button>
                      ) : (
                        <>
                          <Tooltip text="Check for updates">
                            <Button
                              variant="outline"
                              onClick={() => handleCheck(m)}
                              aria-label="Check for updates"
                              className="h-8 px-sm"
                              disabled={!hasToken}
                            >
                              <RefreshCw className="h-4 w-4" aria-hidden="true" />
                            </Button>
                          </Tooltip>
                          <Tooltip
                            text={
                              isModrinth
                                ? "Open project page"
                                : "Project page available only for Modrinth mods"
                            }
                          >
                            <Button
                              variant="outline"
                              as={projectUrl ? "a" : "button"}
                              href={projectUrl || undefined}
                              target={projectUrl ? "_blank" : undefined}
                              rel={projectUrl ? "noopener" : undefined}
                              aria-label="Open project page"
                              className="h-8 px-sm"
                              disabled={!isModrinth}
                            >
                              <ExternalLink
                                className="h-4 w-4"
                                aria-hidden="true"
                              />
                            </Button>
                          </Tooltip>
                          <Tooltip text="Delete mod">
                            <Button
                              variant="outline"
                              onClick={() => handleDelete(m.id)}
                              aria-label="Delete mod"
                              className="h-8 px-sm"
                            >
                              <Trash2 className="h-4 w-4" aria-hidden="true" />
                            </Button>
                          </Tooltip>
                        </>
                      )}
                    </TableCell>
                  </TableRow>
                );
              })}
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
      <Modal open={editOpen} onClose={() => setEditOpen(false)}>
        <form className="space-y-md" onSubmit={saveSettings}>
          <div className="space-y-xs">
            <label htmlFor="inst_name" className="text-sm font-medium">
              Name
            </label>
            <Input
              id="inst_name"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <div className="space-y-xs">
            <span className="text-sm font-medium">Loader</span>
            <div>
              <Badge
                variant="secondary"
                className={`capitalize border ${loaderBadgeClass(instance?.loader)}`}
              >
                {instance?.loader || "unknown"}
              </Badge>
            </div>
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
              onClick={() => setEditOpen(false)}
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
