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
  FileText,
  Download,
  Plus,
  Trash2,
  ExternalLink,
  ArrowLeft,
  Pencil,
  Key,
  AlertTriangle,
  HelpCircle,
  CheckCircle,
  XCircle,
  Hammer,
  Feather,
  Scissors,
  Server,
  MoreVertical,
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
import LoaderSelect from "@/components/LoaderSelect.jsx";
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
  startModUpdate,
  getInstanceLogs,
  getJob,
} from "@/lib/api.ts";
import { cn, summarizeMods } from "@/lib/utils.js";
import { toast } from "@/lib/toast.ts";
import { useConfirm } from "@/hooks/useConfirm.jsx";
import { useOpenAddMod } from "@/hooks/useOpenAddMod.js";
import { useMetaStore } from "@/stores/metaStore.js";

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
  const [editLoader, setEditLoader] = useState("");
  const [mcVersion, setMcVersion] = useState("");
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
  const [updatingId, setUpdatingId] = useState(null);
  // Track live update job state per modId
  const [updateStatus, setUpdateStatus] = useState({}); // { [modId]: { jobId, state, details } }
  const [logsOpen, setLogsOpen] = useState(false);
  const [logs, setLogs] = useState([]);
  const [logsLoading, setLogsLoading] = useState(false);
  const metaLoaders = useMetaStore((s) => s.loaders);
  const metaLoaded = useMetaStore((s) => s.loaded);
  const loadMeta = useMetaStore((s) => s.load);
  const [actionsOpenId, setActionsOpenId] = useState(null);
  // Close mobile actions when clicking outside or pressing Escape
  useEffect(() => {
    function onDocClick(e) {
      const t = e.target;
      if (t.closest?.('[data-actions-menu]') || t.closest?.('[data-actions-trigger]')) return;
      setActionsOpenId(null);
    }
    function onKey(e) { if (e.key === 'Escape') setActionsOpenId(null); }
    if (actionsOpenId != null) {
      document.addEventListener('mousedown', onDocClick);
      document.addEventListener('keydown', onKey);
      return () => {
        document.removeEventListener('mousedown', onDocClick);
        document.removeEventListener('keydown', onKey);
      };
    }
  }, [actionsOpenId]);

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

  // Small icon for loader representation
  function LoaderIcon({ loader, className = "h-4 w-4" }) {
    const key = (loader || "").toLowerCase();
    if (key === "forge") return <Hammer className={cn(className, "text-forge")} aria-hidden title="Forge" />;
    if (key === "fabric") return <Feather className={cn(className, "text-fabric")} aria-hidden title="Fabric" />;
    if (key === "quilt") return <Scissors className={className + " text-purple-600"} aria-hidden title="Quilt" />;
    if (["paper", "spigot", "bukkit"].includes(key)) return <Server className={className + " text-sky-600"} aria-hidden title="Server" />;
    return <Server className={className + " text-muted-foreground"} aria-hidden title={loader || "unknown"} />;
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

  // Ensure loaders metadata is available when edit modal opens
  useEffect(() => {
    if (editOpen && !metaLoaded) {
      loadMeta?.();
    }
  }, [editOpen, metaLoaded, loadMeta]);

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
  const sorted = [...statusFiltered].sort((a, b) => {
    const aOut = (a.available_version || "") !== "" && a.available_version !== a.current_version && !a.virtual;
    const bOut = (b.available_version || "") !== "" && b.available_version !== b.current_version && !b.virtual;
    if (sort === "updates-first") {
      if (aOut !== bOut) return aOut ? -1 : 1; // outdated first
      return a.name.localeCompare(b.name);
    }
    if (sort === "name-desc") return b.name.localeCompare(a.name);
    return a.name.localeCompare(b.name);
  });
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

  async function handleApplyUpdate(m) {
    try {
      setUpdatingId(m.id);
      const key = `${m.id}:${m.available_version || ""}:${crypto.randomUUID?.() || Math.random().toString(36).slice(2)}`;
      const ack = await startModUpdate(m.id, key);
      trackUpdateJob(ack.job_id, m.id);
    } catch (err) {
      setUpdatingId(null);
      toast.error(err instanceof Error ? err.message : "Failed to start update");
    }
  }

  function trackUpdateJob(jobId, modId) {
    const es = new EventSource(`/api/jobs/${jobId}/events`);
    setSource(es);
    const onState = (e) => {
      try {
        const payload = JSON.parse(e.data);
        const st = (payload?.state || "").toLowerCase();
        setUpdateStatus((prev) => ({ ...prev, [modId]: { jobId, state: st, details: payload?.details || {} } }));
        if (["succeeded", "failed", "partialsuccess"].includes(st)) {
          es.close();
          setSource(null);
          setUpdatingId(null);
          setUpdateStatus((prev) => { const next = { ...prev }; delete next[modId]; return next; });
          fetchMods();
          fetchInstance();
          if (st === "succeeded") {
            const ver = payload?.details?.version;
            toast.success(ver ? `Updated to v${ver}` : "Update applied");
          } else if (st === "partialsuccess") {
            toast.warning(payload?.details?.hint || "Update partially applied; manual cleanup may be required.");
          } else {
            const msg = payload?.details?.hint || payload?.details?.error || "Update failed";
            toast.error(`${msg} — see Logs for details`);
            // Open logs modal to help user inspect recent actions
            openLogs();
          }
        }
      } catch {
        // ignore bad events
      }
    };
    es.addEventListener("state", onState);
    es.onerror = () => {
      // Do not close; allow EventSource to auto-reconnect.
      // Also start polling as a fallback to keep UI updated until reconnection.
      pollUpdateJob(jobId, modId);
    };
  }

  async function pollUpdateJob(jobId, modId) {
    let stopped = false;
    while (!stopped) {
      try {
        const data = await getJob(jobId);
        // Handle both update-job shape {state} and sync-job shape {status}
        const st = (data?.state || data?.status || "").toString().toLowerCase();
        if (st) {
          setUpdateStatus((prev) => ({ ...prev, [modId]: { jobId, state: st, details: data?.details || {} } }));
        }
        if (["succeeded", "failed", "partialsuccess", "canceled"].includes(st)) {
          setUpdatingId(null);
          setUpdateStatus((prev) => { const next = { ...prev }; delete next[modId]; return next; });
          fetchMods();
          fetchInstance();
          if (st === "succeeded") {
            const ver = data?.details?.version;
            toast.success(ver ? `Updated to v${ver}` : "Update applied");
          } else if (st === "partialsuccess") {
            toast.warning(data?.details?.hint || "Update partially applied; manual cleanup may be required.");
          } else {
            const msg = data?.details?.hint || data?.details?.error || "Update failed";
            toast.error(`${msg} — see Logs for details`);
            openLogs();
          }
          stopped = true;
          break;
        }
      } catch (err) {
        // ignore transient errors; continue polling
      }
      await new Promise((res) => setTimeout(res, 1500));
    }
  }

  function UpdateStepper({ modId }) {
    const st = updateStatus[modId]?.state || "";
    const order = [
      { key: "uploadingnew", label: "Uploading" },
      { key: "verifyingnew", label: "Verifying" },
      { key: "updatingdb", label: "Updating DB" },
      { key: "removingold", label: "Removing old" },
      { key: "verifyingremoval", label: "Verifying removal" },
    ];
    if (!st) return null;
    const idx = order.findIndex((s) => s.key === st);
    return (
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        {order.map((s, i) => (
          <div key={s.key} className="flex items-center gap-1">
            <span className={cn(
              "inline-block h-2.5 w-2.5 rounded-full",
              i < idx ? "bg-emerald-500" : i === idx ? "bg-emerald-400 animate-pulse" : "bg-muted"
            )} />
            <span className={cn(i === idx ? "text-foreground" : i < idx ? "text-emerald-700" : "")}>{s.label}</span>
            {i < order.length - 1 && <span className="mx-1 opacity-50">›</span>}
          </div>
        ))}
      </div>
    );
  }

  async function saveSettings(e) {
    e.preventDefault();
    if (!name.trim()) {
      toast.error("Name required");
      return;
    }
    if (!editLoader.trim()) {
      toast.error("Loader required");
      return;
    }
    try {
      const payload = { name };
      const v = mcVersion.trim();
      if (v !== "") Object.assign(payload, { gameVersion: v });
      const ld = editLoader.trim();
      if (ld !== "") Object.assign(payload, { loader: ld });
      const updated = await updateInstance(instance.id, payload);
      setInstance(updated);
      toast.success("Instance updated");
      setEditOpen(false);
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Failed to save instance",
      );
    }
  }

  async function openLogs() {
    setLogsOpen(true);
    setLogsLoading(true);
    try {
      const data = await getInstanceLogs(instanceId, 250);
      setLogs(data || []);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load logs");
    } finally {
      setLogsLoading(false);
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
    <div className="grid gap-xl">
      {ConfirmModal}
      {instance?.requires_loader && (
        <div className="rounded-md border border-red-300 bg-red-50 p-sm text-red-800 flex items-center justify-between">
          <div className="text-sm">This instance requires a loader to be set before actions are available.</div>
          <Button size="sm" variant="outline" onClick={() => { setName(instance.name); setEditLoader(instance.loader || ''); setEditOpen(true); }}>
            Set loader
          </Button>
        </div>
      )}
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
      {/* Top: Instance header + server info card */}
      <section className="rounded-lg border bg-muted/40 p-md">
        <div className="space-y-sm min-w-0">
          <Link
            to="/instances"
            className="inline-flex items-center gap-xs text-sm text-muted-foreground hover:underline"
          >
            <ArrowLeft className="h-4 w-4" aria-hidden="true" />
            Back to Instances
          </Link>
          {instance && (
            <>
              <div className="flex items-center gap-sm min-w-0">
                <h1 className="text-3xl md:text-4xl font-bold tracking-tight truncate max-w-full">
                  {instance.name}
                  {nameSuffix ? ` (${nameSuffix})` : ""}
                </h1>
                <Button
                  variant="outline"
                  className="h-9 w-9 p-0"
                  aria-label="Edit instance"
                  title="Edit instance"
                  onClick={() => {
                    setName(instance.name);
                    setMcVersion(instance.gameVersionSource === 'manual' ? (instance.gameVersion || '') : '');
                    setEditLoader(instance.loader || '');
                    setEditOpen(true);
                  }}
                >
                  <Pencil className="h-5 w-5 md:h-6 md:w-6 text-foreground" aria-hidden />
                </Button>
              </div>
              <div className="mt-xs flex flex-wrap items-center gap-md">
                <div className="inline-flex items-center gap-2">
                  <span className="text-sm text-muted-foreground">Loader:</span>
                  <Badge
                    variant="secondary"
                    className={`capitalize border ${loaderBadgeClass(instance.loader)}`}
                  >
                    {instance.loader || "unknown"}
                  </Badge>
                </div>
                <div className="inline-flex items-center gap-xs">
                  <span className="text-sm text-muted-foreground">Game version:</span>
                  <span className="text-sm font-medium">
                    {instance.gameVersion?.trim() || "Unknown"}
                  </span>
                  {instance.gameVersionSource === 'pufferpanel' ? (
                    <Badge variant="secondary" className="border bg-sky-50 text-sky-800 border-sky-200">Synced</Badge>
                  ) : instance.gameVersionSource === 'manual' ? (
                    <Badge variant="secondary" className="border bg-emerald-50 text-emerald-800 border-emerald-200">Manual</Badge>
                  ) : null}
                  {instance.gameVersionKey ? (
                    <Tooltip text={`PufferPanel variable: ${instance.gameVersionKey}`}>
                      <button type="button" className="text-muted-foreground" aria-label="Show version key">
                        <HelpCircle className="h-4 w-4" aria-hidden />
                      </button>
                    </Tooltip>
                  ) : null}
                </div>
              </div>
            </>
          )}
        </div>
      </section>

      {/* Middle: Status overview + actions */}
      <section className="grid gap-lg lg:grid-cols-3 items-stretch">
        <div className="lg:col-span-2">
          {instance && (
            <>
              {loading ? (
                <div className="grid grid-cols-1 gap-sm sm:grid-cols-3 max-w-3xl sm:max-w-4xl">
                  <Skeleton className="h-24 rounded-md shadow-sm" />
                  <Skeleton className="h-24 rounded-md shadow-sm" />
                  <Skeleton className="h-24 rounded-md shadow-sm" />
                </div>
              ) : (
                (() => {
                  const s = summarizeMods(withVirtuals);
                  return (
                    <div className="grid grid-cols-1 gap-sm sm:grid-cols-3 max-w-3xl sm:max-w-4xl" role="region" aria-label="Instance status overview">
                      <div className="rounded-lg border p-lg shadow-sm border-l-4 border-l-emerald-400 bg-background">
                        <div className="flex items-center gap-sm">
                          <CheckCircle className="h-6 w-6 text-emerald-600" aria-hidden />
                          <div className="flex items-baseline gap-xs">
                            <span className="text-3xl font-extrabold leading-none text-emerald-700">{s.mods_up_to_date}</span>
                            <span className="text-sm text-muted-foreground">Up to date</span>
                          </div>
                        </div>
                      </div>
                      <div className="rounded-lg border p-lg shadow-sm border-l-4 border-l-amber-400 bg-background">
                        <div className="flex items-center gap-sm">
                          <RefreshCw className="h-6 w-6 text-amber-600" aria-hidden />
                          <div className="flex items-baseline gap-xs">
                            <span className="text-3xl font-extrabold leading-none text-amber-700">{s.mods_update_available}</span>
                            <span className="text-sm text-muted-foreground">Updates available</span>
                          </div>
                        </div>
                      </div>
                      <div className="rounded-lg border p-lg shadow-sm border-l-4 border-l-red-400 bg-background">
                        <div className="flex items-center gap-sm">
                          <XCircle className="h-6 w-6 text-red-600" aria-hidden />
                          <div className="flex items-baseline gap-xs">
                            <span className="text-3xl font-extrabold leading-none text-red-700">{s.mods_failed}</span>
                            <span className="text-sm text-muted-foreground">Failed</span>
                          </div>
                        </div>
                      </div>
                    </div>
                  );
                })()
              )}
              {(checkingAll || resyncing) && (
                <div className="mt-sm" aria-live="polite">
                  <div className="mb-xs flex items-center justify-between text-sm text-muted-foreground">
                    <span>{checkingAll ? "Checking updates" : "Syncing instance"}</span>
                    {checkingAll && (
                      <span>
                        {checkProgress} / {mods.length}
                      </span>
                    )}
                  </div>
                  <div className="h-2 w-full rounded bg-muted overflow-hidden">
                    {checkingAll ? (
                      <div
                        className="h-full bg-emerald-500 transition-all"
                        style={{ width: `${Math.max(0, Math.min(100, Math.round(((checkProgress || 0) / Math.max(1, mods.length)) * 100)))}%` }}
                      />
                    ) : (
                      <div className="h-full w-1/3 bg-emerald-500 animate-pulse" />
                    )}
                  </div>
                </div>
              )}
            </>
          )}
        </div>
        <div className="rounded-lg border bg-muted/20 p-md shadow-sm w-full lg:w-fit lg:justify-self-end h-full flex flex-col" aria-label="Actions">
          <div className="flex flex-wrap items-center gap-sm">
            <Button
              size="sm"
              variant="outline"
              onClick={handleCheckAll}
              disabled={instance?.requires_loader || checkingAll || !hasToken || mods.length === 0}
              className="gap-xs disabled:opacity-50 disabled:pointer-events-none"
              title={instance?.requires_loader ? "Set loader first" : undefined}
            >
              <RefreshCw className="h-4 w-4" aria-hidden />
              Check Updates
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={openLogs}
              className="gap-xs"
            >
              <FileText className="h-4 w-4" aria-hidden />
              Logs
            </Button>
            {/* Edit action removed from this card; moved next to header */}
            {instance?.pufferpanel_server_id && (
              <Button
                size="sm"
                variant="outline"
                onClick={handleResync}
                disabled={instance?.requires_loader || resyncing}
                className="gap-xs disabled:opacity-50 disabled:pointer-events-none"
                title={instance?.requires_loader ? "Set loader first" : undefined}
              >
                <RotateCw className="h-4 w-4" aria-hidden />
                {resyncing ? "Resyncing" : "Resync"}
              </Button>
            )}
          </div>
          {!hasToken && (
            <div
              className="mt-sm flex items-center gap-sm rounded-md border border-yellow-200 bg-yellow-50 p-sm text-yellow-800"
              role="status"
            >
              <Key className="h-4 w-4" aria-hidden />
              <span className="text-sm">
                Set a Modrinth token in Settings to enable update checks.
              </span>
            </div>
          )}
        </div>
        {unmatched.length > 0 && (
          <div className="md:col-span-3">
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
      </section>

      {/* Divider between actions and mods list */}
      <div className="h-px bg-border/60 rounded" />

      {/* Bottom: Mods table */}
      <section className="grid gap-lg">
        <div className="rounded-lg border bg-muted/20 p-md shadow-sm">
          <div className="flex flex-wrap items-center gap-sm">
            <div className="flex flex-col min-w-[220px] sm:w-64">
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
            <div className="flex flex-col min-w-[160px] sm:w-40">
              <label htmlFor="sort" className="sr-only">
                Sort mods
              </label>
          <Select
            id="sort"
            value={sort}
            onChange={(e) => setSort(e.target.value)}
            className="sm:w-40"
          >
            <option value="name-asc">Name A-Z</option>
            <option value="name-desc">Name Z-A</option>
            <option value="updates-first">Updates first</option>
          </Select>
            </div>
            <div className="ml-auto">
              <Button
                onClick={openAddMod}
                className={cn(
                  "gap-xs",
                  !hasToken && "pointer-events-none opacity-50",
                )}
                disabled={!hasToken}
                title="Add Mod"
              >
                <Plus className="h-4 w-4" aria-hidden="true" />
                Add Mod
              </Button>
            </div>
          </div>
        </div>

        {loading && (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Game Version</TableHead>
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

        {/* Primary data view rendered below (responsive list/table). Removed legacy duplicate table. */}
      </section>
        {/* Removed duplicated unmatched/controls/skeleton/empty blocks to prevent double rendering */}
        {!loading && !error && current.length > 0 && (
          <>
            {/* Small screens: card list */}
            <div className="grid gap-sm sm:hidden">
              {current.map((m) => {
                const projectUrl = getProjectUrl(m);
                const isModrinth = !!projectUrl;
                const outdated =
                  (m.available_version || "") !== "" && m.available_version !== m.current_version;
                const open = actionsOpenId === m.id;
                return (
                  <div key={m.id} className={cn("relative rounded-lg border p-md shadow-sm", outdated && "bg-emerald-50/60")}> 
                    <div className="flex items-start gap-sm">
                      <ModIcon
                        url={m.icon_url}
                        cacheKey={
                          m.available_version ||
                          m.current_version ||
                          String(m.id)
                        }
                      />
                      <div className="min-w-0 flex-1">
                        <div className="flex items-start gap-sm">
                          <div className="min-w-0 flex-1">
                            <div className="flex items-center gap-xs">
                              <span className="font-semibold truncate">{m.name || m.url}</span>
                              {!m.virtual && outdated && (
                                <Badge variant="secondary" className="border border-amber-200 bg-amber-100 text-amber-800">Update available</Badge>
                              )}
                              {m.virtual && (
                                <span className="inline-flex items-center gap-1 text-xs text-red-600" role="status">
                                  <AlertTriangle className="h-4 w-4" aria-hidden />
                                  Could not be matched
                                </span>
                              )}
                            </div>
                            <div className="mt-1 flex flex-wrap items-center gap-sm text-sm text-muted-foreground">
                              <span className="inline-flex items-center gap-1"><LoaderIcon loader={m.loader} /><span className="sr-only">{m.loader}</span></span>
                              {m.game_version && <span>Game {m.game_version}</span>}
                              {m.current_version && (
                                <span>
                                  Current {m.current_version}
                                  {m.available_version && ` • Available ${m.available_version}`}
                                </span>
                              )}
                            </div>
                          </div>
                          <div className="shrink-0 relative">
                            <Button
                              variant="outline"
                              className="h-8 px-sm"
                              onClick={() => setActionsOpenId(open ? null : m.id)}
                              aria-haspopup="menu"
                              aria-expanded={open}
                              aria-label="Actions"
                              data-actions-trigger
                            >
                              <MoreVertical className="h-4 w-4" aria-hidden />
                            </Button>
                            {open && (
                              <div className="absolute right-0 z-10 mt-1 w-44 rounded-md border bg-background p-xs shadow-md" role="menu" data-actions-menu>
                                {m.virtual ? (
                                  <Button
                                    variant="outline"
                                    className="w-full justify-start h-8 px-sm"
                                    onClick={() => { setActionsOpenId(null); openAddMod(m.file || m.name); }}
                                  >
                                    Match mod
                                  </Button>
                                ) : (
                                  <div className="grid gap-xs">
                                    {outdated && (
                                      <Button
                                        variant="outline"
                                        className="w-full justify-start h-8 px-sm"
                                        onClick={() => { setActionsOpenId(null); handleApplyUpdate(m); }}
                                        disabled={updatingId === m.id || !!updateStatus[m.id]}
                                      >
                                        <Download className="h-4 w-4 mr-1" /> Apply update
                                      </Button>
                                    )}
                                    <Button
                                      variant="outline"
                                      as={isModrinth ? "a" : "button"}
                                      href={projectUrl || undefined}
                                      target={projectUrl ? "_blank" : undefined}
                                      rel={projectUrl ? "noopener" : undefined}
                                      className="w-full justify-start h-8 px-sm"
                                      onClick={() => setActionsOpenId(null)}
                                      disabled={!isModrinth}
                                    >
                                      <ExternalLink className="h-4 w-4 mr-1" /> Open project
                                    </Button>
                                    <Button
                                      variant="outline"
                                      className="w-full justify-start h-8 px-sm"
                                      onClick={() => { setActionsOpenId(null); handleDelete(m.id); }}
                                    >
                                      <Trash2 className="h-4 w-4 mr-1" /> Delete
                                    </Button>
                                  </div>
                                )}
                              </div>
                            )}
                          </div>
                        </div>
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>

            {/* Desktop/tablet: table view */}
            <div className="hidden sm:block">
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
                    const outdated =
                      (m.available_version || "") !== "" && m.available_version !== m.current_version;
                    return (
                      <TableRow key={m.id} className={cn("group", outdated && "bg-emerald-50/60")}> 
                        <TableCell className="flex items-center gap-sm font-medium text-base">
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
                            {!m.virtual && outdated && (
                              <Badge
                                variant="secondary"
                                className="ml-1 border border-amber-200 bg-amber-100 text-amber-800"
                              >
                                Update available
                              </Badge>
                            )}
                            {m.virtual && (
                              <span className="inline-flex items-center gap-1 text-xs text-red-600" role="status">
                                <AlertTriangle className="h-4 w-4" aria-hidden />
                                Could not be matched
                              </span>
                            )}
                          </span>
                        </TableCell>
                        <TableCell className="text-sm text-muted-foreground">{m.game_version}</TableCell>
                        <TableCell>
                          <span className="inline-flex items-center gap-1">
                            <LoaderIcon loader={m.loader} />
                            <span className="sr-only">{m.loader}</span>
                          </span>
                        </TableCell>
                        <TableCell className="text-sm">{m.current_version}</TableCell>
                        <TableCell className="text-sm">{m.available_version}</TableCell>
                        <TableCell className="flex gap-xs">
                          {m.virtual ? (
                            <Button
                              variant="outline"
                              onClick={() => openAddMod(m.file || m.name)}
                              aria-label="Match mod"
                              className="h-8 px-sm transition-colors group-hover:bg-muted group-hover:border-foreground/20"
                            >
                              Match mod
                            </Button>
                          ) : (
                            <>
                              {outdated && (
                                <Tooltip text="Apply update">
                                  <Button
                                    variant="outline"
                                    onClick={() => handleApplyUpdate(m)}
                                    aria-label="Apply update"
                                    className="h-8 px-sm border-emerald-300 text-emerald-700 hover:bg-emerald-100 transition-colors group-hover:bg-emerald-50 group-hover:border-emerald-300"
                                    disabled={updatingId === m.id || !!updateStatus[m.id]}
                                  >
                                    <Download
                                      className={cn(
                                        "h-4 w-4",
                                        (updatingId === m.id || updateStatus[m.id]) && "animate-bounce"
                                      )}
                                      aria-hidden="true"
                                    />
                                  </Button>
                                </Tooltip>
                              )}
                              {!!updateStatus[m.id] && (
                                <div className="ml-2">
                                  <UpdateStepper modId={m.id} />
                                </div>
                              )}
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
                                  className="h-8 px-sm transition-colors group-hover:bg-muted group-hover:border-foreground/20"
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
                                  className="h-8 px-sm transition-colors group-hover:bg-muted group-hover:border-foreground/20"
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
            </div>

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
            <label htmlFor="mc_version" className="text-sm font-medium">Minecraft version</label>
            <Input
              id="mc_version"
              placeholder={instance?.gameVersion?.trim() || "e.g. 1.21.1"}
              value={mcVersion}
              onChange={(e) => setMcVersion(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              Setting a value stores a manual version for this instance. Leave empty to keep the current value.
            </p>
          </div>
          <div className="space-y-xs">
            <label className="text-sm font-medium">Loader</label>
            <LoaderSelect loaders={metaLoaders} value={editLoader} onChange={setEditLoader} disabled={!metaLoaded || metaLoaders.length === 0} />
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
      <Modal open={logsOpen} onClose={() => setLogsOpen(false)} className="max-w-5xl w-[92vw]">
        <div className="space-y-sm">
          <div className="flex items-center justify-between gap-sm">
            <h2 className="text-lg font-medium">Activity Logs</h2>
            <Button variant="secondary" size="sm" onClick={() => setLogsOpen(false)}>Close</Button>
          </div>
          {logsLoading ? (
            <Skeleton className="h-32 w-full" />
          ) : logs.length === 0 ? (
            <p className="text-sm text-muted-foreground">No activity yet.</p>
          ) : (
            <div className="max-h-96 overflow-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Time</TableHead>
                    <TableHead>Action</TableHead>
                    <TableHead>Mod</TableHead>
                    <TableHead>Details</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {logs.map((ev) => {
                    const when = new Date(ev.created_at).toLocaleString();
                    const color =
                      ev.action === "added"
                        ? "text-emerald-600"
                        : ev.action === "deleted"
                          ? "text-red-600"
                          : "text-sky-600";
                    const Icon = ev.action === "added" ? Plus : ev.action === "deleted" ? Trash2 : Download;
                    return (
                      <TableRow key={ev.id}>
                        <TableCell className="whitespace-nowrap text-sm text-muted-foreground">{when}</TableCell>
                        <TableCell className="text-sm">
                          <span className={cn("inline-flex items-center gap-1", color)}>
                            <Icon className="h-4 w-4" aria-hidden />
                            {ev.action}
                          </span>
                        </TableCell>
                        <TableCell className="text-sm">{ev.mod_name}</TableCell>
                        <TableCell className="text-sm whitespace-nowrap">
                          {ev.action === "updated" && ev.from_version
                            ? `${ev.from_version} → ${ev.to_version || ""}`
                            : ev.action === "added"
                              ? ev.to_version
                              : ev.from_version}
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </div>
          )}
        </div>
      </Modal>
    </div>
  );
}
