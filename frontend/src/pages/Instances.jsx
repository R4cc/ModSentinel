import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Server, Trash2, Key, Plug, Package } from "lucide-react";
import { Badge } from "@/components/ui/Badge.jsx";
import { Button } from "@/components/ui/Button.jsx";
import { Modal } from "@/components/ui/Modal.jsx";
import { Input } from "@/components/ui/Input.jsx";
import { Select } from "@/components/ui/Select.jsx";
import { Checkbox } from "@/components/ui/Checkbox.jsx";
import { Skeleton } from "@/components/ui/Skeleton.jsx";
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  CardFooter,
} from "@/components/ui/Card.jsx";
import { EmptyState } from "@/components/ui/EmptyState.jsx";
import {
  getInstances,
  addInstance,
  updateInstance,
  deleteInstance,
  getSecretStatus,
  syncInstances,
  getPufferServers,
} from "@/lib/api.ts";
import { toast } from "@/lib/toast.ts";
import { jobs } from "@/lib/api.ts";

const loaders = [
  { id: "fabric", label: "Fabric" },
  { id: "forge", label: "Forge" },
  { id: "quilt", label: "Quilt" },
];

export default function Instances() {
  const [instances, setInstances] = useState([]);
  const [suffixMap, setSuffixMap] = useState({}); // id -> index
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState(null);
  const [addTab, setAddTab] = useState("local"); // 'local' | 'puffer'
  const [name, setName] = useState("");
  const [nameError, setNameError] = useState("");
  const [loader, setLoader] = useState(loaders[0].id);
  const [enforce, setEnforce] = useState(true);
  const [hasToken, setHasToken] = useState(true);
  const [hasPuffer, setHasPuffer] = useState(false);
  const [pufferLoaded, setPufferLoaded] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [servers, setServers] = useState([]);
  const [selectedServer, setSelectedServer] = useState("");
  const [loadingServers, setLoadingServers] = useState(false);
  const [serverError, setServerError] = useState("");
  const [scanning, setScanning] = useState(false);
  const [jobProgress, setJobProgress] = useState(null);
  const [jobSource, setJobSource] = useState(null);
  const [showFailures, setShowFailures] = useState(false);

  useEffect(() => {
    return () => {
      if (jobSource) jobSource.close();
    };
  }, [jobSource]);
  const navigate = useNavigate();

  useEffect(() => {
    fetchInstances();
  }, []);

  useEffect(() => {
    function check() {
      setPufferLoaded(false);
      getSecretStatus("pufferpanel")
        .then((s) => setHasPuffer(s.exists))
        .catch(() => setHasPuffer(false))
        .finally(() => setPufferLoaded(true));
    }
    check();
    window.addEventListener("pufferpanel-change", check);
    return () => window.removeEventListener("pufferpanel-change", check);
  }, []);

  useEffect(() => {
    getSecretStatus("modrinth")
      .then((s) => setHasToken(s.exists))
      .catch(() => setHasToken(false));
  }, []);

  async function fetchServers() {
    setLoadingServers(true);
    setServerError("");
    try {
      const s = await getPufferServers();
      setServers(s);
    } catch (err) {
      setServerError(
        err instanceof Error ? err.message : "Failed to load servers",
      );
    } finally {
      setLoadingServers(false);
    }
  }

  async function fetchInstances() {
    setLoading(true);
    setError("");
    try {
      const data = await getInstances();
      setInstances(data);
      computeSuffixes(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load instances");
    } finally {
      setLoading(false);
    }
  }

  function computeSuffixes(list) {
    const groups = new Map();
    for (const i of list) {
      const key = i.name || "";
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key).push(i);
    }
    const map = {};
    for (const [_, arr] of groups) {
      if (arr.length <= 1) continue;
      arr.sort((a, b) => {
        const ta = Date.parse(a.created_at || "");
        const tb = Date.parse(b.created_at || "");
        if (!isNaN(ta) && !isNaN(tb) && ta !== tb) return ta - tb;
        return (a.id || 0) - (b.id || 0);
      });
      arr.forEach((inst, idx) => {
        if (idx > 0) map[inst.id] = idx; // first has no suffix
      });
    }
    setSuffixMap(map);
  }

  async function handleSync() {
    setSyncing(true);
    try {
      await Promise.all(
        instances
          .filter((i) => i.pufferpanel_server_id)
          .map((i) => syncInstances(i.pufferpanel_server_id, i.id)),
      );
      toast.success("Synced");
      fetchInstances();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to sync");
    } finally {
      setSyncing(false);
    }
  }

  function openAdd() {
    setEditing(null);
    setName("");
    setNameError("");
    setLoader(loaders[0].id);
    setEnforce(true);
    setServers([]);
    setSelectedServer("");
    setServerError("");
    setAddTab("local");
    setOpen(true);
    if (pufferLoaded && hasPuffer) fetchServers();
  }

  function openEdit(inst) {
    setEditing(inst);
    setName(inst.name);
    setNameError("");
    setEnforce(inst.enforce_same_loader);
    setSelectedServer(inst.pufferpanel_server_id || "");
    setServerError("");
    setOpen(true);
    if (pufferLoaded && hasPuffer) fetchServers();
  }

  async function handleSave(e) {
    e.preventDefault();
    const localMode = addTab === "local";
    const pufferMode = addTab === "puffer";
    if (localMode && !name.trim()) {
      toast.error("Name required");
      return;
    }
    if (pufferMode) {
      if (!selectedServer) {
        toast.error("Select a PufferPanel server");
        return;
      }
      try {
        setScanning(true);
        setJobProgress(null);
        // Determine target instance id
        let targetId = editing?.id;
        if (!editing) {
          const serverName = (servers.find((s) => s.id === selectedServer)?.name) || "";
          const finalName = name.trim() || serverName;
          const created = await addInstance({
            name: finalName,
            loader: "",
            enforce_same_loader: true,
            pufferpanel_server_id: selectedServer,
          });
          targetId = created.id;
        }
        const job = await syncInstances(selectedServer, targetId);
        // Track progress via SSE and show inline in the modal
        const es = new EventSource(`/api/jobs/${job.id}/events`);
        setJobSource(es);
        // Close modal and refresh list immediately so the new instance appears
        setOpen(false);
        fetchInstances();
        es.onmessage = (ev) => {
          const data = JSON.parse(ev.data);
          setJobProgress(data);
          if (
            data.status === "succeeded" ||
            data.status === "failed" ||
            data.status === "canceled"
          ) {
            es.close();
            setJobSource(null);
            setScanning(false);
            fetchInstances();
            if (data.status === "succeeded") {
              toast.success("Resynced");
              setOpen(false);
              const unmatched = Array.isArray(data.failures)
                ? Array.from(new Set(data.failures.map((f) => f.name).filter(Boolean)))
                : [];
              navigate(`/instances/${targetId}`,
                unmatched.length > 0 ? { state: { unmatched } } : undefined,
              );
            } else if (data.status === "failed") {
              toast.error("Sync failed");
            } else {
              toast.error("Sync canceled");
            }
          }
        };
      } catch (err) {
        toast.error(err instanceof Error ? err.message : "Failed to sync");
        // Close the modal on error to avoid a stuck state
        setOpen(false);
      } finally {
        setScanning(false);
      }
      return;
    }

    if (editing) {
      try {
        const updated = await updateInstance(editing.id, { name });
        setInstances((prev) =>
          prev.map((i) => (i.id === updated.id ? { ...i, ...updated } : i)),
        );
        toast.success("Instance updated");
        setOpen(false);
      } catch (err) {
        toast.error(
          err instanceof Error ? err.message : "Failed to save instance",
        );
      }
      return;
    }

    const tempId = Date.now();
    const optimistic = {
      id: tempId,
      name,
      loader,
      enforce_same_loader: enforce,
      mod_count: 0,
    };
    setInstances((prev) => [...prev, optimistic]);
    setOpen(false);
    try {
      const created = await addInstance({
        name,
        loader,
        enforce_same_loader: enforce,
      });
      setInstances((prev) =>
        prev.map((i) => (i.id === tempId ? { ...created, mod_count: 0 } : i)),
      );
      toast.success("Instance added");
      navigate(`/instances/${created.id}`);
    } catch (err) {
      setInstances((prev) => prev.filter((i) => i.id !== tempId));
      toast.error(
        err instanceof Error ? err.message : "Failed to save instance",
      );
    }
  }

  const [delState, setDelState] = useState({ open: false, inst: null });

  function openDelete(inst) { setDelState({ open: true, inst }); }

  async function handleDelete(e) {
    e.preventDefault();
    const { inst } = delState;
    if (!inst) return;
    try {
      await deleteInstance(inst.id);
      toast.success("Instance deleted");
      setDelState({ open: false, inst: null });
      fetchInstances();
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Failed to delete instance",
      );
    }
  }

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

  return (
    <div className="space-y-md">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">Instances</h1>
      </div>
      {/* Sync indicator card when a job is running or just finished from this page */}
      {jobProgress && (
        <div className="rounded border p-sm space-y-xs w-full max-w-3xl">
          <div className="flex items-center justify-between gap-sm">
            <div className="flex items-center gap-sm">
              <RotateCw
                className={
                  jobProgress.status === "running" ? "h-4 w-4 animate-spin" : "h-4 w-4 opacity-60"
                }
                aria-hidden
              />
              <p className="text-sm font-medium">
                {jobProgress.status === "running" ? "Syncing from PufferPanel" : "Sync summary"}
              </p>
            </div>
          </div>
          {jobProgress.status === "running" ? (
            <>
              <div
                className="h-2 bg-muted rounded"
                role="progressbar"
                aria-valuemin={0}
                aria-valuemax={jobProgress.total}
                aria-valuenow={jobProgress.processed}
              >
                <div
                  className="h-2 bg-primary rounded transition-[width] duration-300"
                  style={{
                    width: jobProgress.total
                      ? `${(jobProgress.processed / jobProgress.total) * 100}%`
                      : "0%",
                  }}
                />
              </div>
              <p className="text-sm">
                {jobProgress.processed}/{jobProgress.total} processed (
                {jobProgress.succeeded} succeeded, {jobProgress.failed} failed)
              </p>
            </>
          ) : (
            <>
              {(() => {
                const total = jobProgress.total || 0;
                const ok = jobProgress.succeeded || 0;
                const fail = jobProgress.failed || 0;
                const okPct = total ? (ok / total) * 100 : 0;
                const failPct = total ? (fail / total) * 100 : 0;
                return (
                  <div className="h-2 bg-muted rounded overflow-hidden">
                    <div className="h-2 bg-emerald-500 inline-block" style={{ width: `${okPct}%` }} />
                    <div className="h-2 bg-red-500 inline-block" style={{ width: `${failPct}%` }} />
                  </div>
                );
              })()}
              <div className="flex items-center justify-between">
                <p className="text-sm">
                  {jobProgress.succeeded} succeeded, {jobProgress.failed} failed
                </p>
                {jobProgress.failed > 0 && (
                  <Button size="sm" variant="secondary" onClick={() => setShowFailures((v) => !v)}>
                    {showFailures ? "Hide failures" : "View failures"}
                  </Button>
                )}
              </div>
            </>
          )}
          {showFailures && jobProgress.failures?.length > 0 && (
            <div className="max-h-48 overflow-auto border rounded p-xs text-sm">
              {jobProgress.failures.map((f, i) => (
                <div key={i} className="flex justify-between gap-sm">
                  <span className="truncate" title={f.name}>{f.name}</span>
                  <span className="text-destructive truncate" title={f.error}>{f.error}</span>
                </div>
              ))}
              {jobProgress.status !== "running" && (
                <div className="text-right mt-xs">
                  <Button size="sm" onClick={async () => { try { await jobs.retry(jobProgress.id); } catch (e) { toast.error(e instanceof Error ? e.message : "Failed to retry"); } }}>Retry failed</Button>
                </div>
              )}
            </div>
          )}
        </div>
      )}
      <div className="grid gap-sm md:grid-cols-2 w-full max-w-3xl">
        {!hasToken && (
          <Card className="p-sm bg-yellow-50 border-yellow-200 text-yellow-800">
            <div className="flex items-center gap-sm">
              <Key className="h-4 w-4" aria-hidden />
              <div>
                <p className="text-sm font-medium">Modrinth token missing</p>
                <p className="text-xs">
                  Set a Modrinth token in Settings to enable update checks.
                </p>
              </div>
            </div>
          </Card>
        )}
        {pufferLoaded && !hasPuffer && (
          <Card className="p-sm bg-yellow-50 border-yellow-200 text-yellow-800">
            <div className="flex items-center gap-sm">
              <Plug className="h-4 w-4" aria-hidden />
              <div>
                <p className="text-sm font-medium">PufferPanel not connected</p>
                <p className="text-xs">
                  Set PufferPanel credentials in Settings to enable sync.
                </p>
              </div>
            </div>
          </Card>
        )}
      </div>
      <div className="flex justify-end md:justify-between gap-sm">
        <Button onClick={openAdd}>Add instance</Button>
        {pufferLoaded && hasPuffer && (
          <Button
            variant="secondary"
            onClick={handleSync}
            disabled={syncing}
            aria-busy={syncing}
          >
            {syncing ? "Syncing..." : "Sync"}
          </Button>
        )}
      </div>
      {loading && (
        <div className="grid grid-cols-1 gap-md sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <Card key={i} className="p-md space-y-sm">
              <Skeleton className="h-5 w-32" />
              <Skeleton className="h-4 w-20" />
              <Skeleton className="h-4 w-8" />
            </Card>
          ))}
        </div>
      )}
      {!loading && error && (
        <div className="flex flex-col items-center gap-sm">
          <p className="text-sm text-muted-foreground">{error}</p>
          <Button onClick={fetchInstances}>Retry</Button>
        </div>
      )}
      {!loading && !error && instances.length === 0 && (
        <EmptyState
          icon={Server}
          title="No instances"
          message="You haven't added any instances yet."
        />
      )}
      {!loading && !error && instances.length > 0 && (
        <div
          className="grid grid-cols-1 gap-md sm:grid-cols-2 lg:grid-cols-3"
          data-testid="instance-grid"
        >
          {instances.map((inst) => (
            <Card
              key={inst.id}
              role="link"
              tabIndex={0}
              onClick={() => navigate(`/instances/${inst.id}`)}
              onKeyDown={(e) => {
                if (e.key === "Enter") navigate(`/instances/${inst.id}`);
              }}
              aria-label={inst.name}
              className="flex flex-col justify-between cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 transition-colors hover:border-primary/40 hover:shadow-lg"
            >
              <CardHeader className="pb-0">
                <div className="flex items-center justify-between gap-sm">
                  <CardTitle className="truncate">
                    {inst.name}
                    {suffixMap[inst.id] ? ` (${suffixMap[inst.id]})` : ""}
                  </CardTitle>
                  <div className="flex items-center gap-xs">
                    {inst.pufferpanel_server_id && (
                      <Badge variant="secondary" className="border bg-sky-50 text-sky-800 border-sky-200">
                        PufferPanel
                      </Badge>
                    )}
                    <Badge
                      variant="secondary"
                      className={`capitalize border ${loaderBadgeClass(inst.loader)}`}
                    >
                      {inst.loader || "unknown"}
                    </Badge>
                  </div>
                </div>
              </CardHeader>
              <CardContent className="pt-sm text-sm text-muted-foreground flex items-center justify-start">
                <span className="flex items-center gap-xs">
                  <Package className="h-4 w-4" aria-hidden />
                  {inst.mod_count} mods
                </span>
              </CardContent>
              <CardFooter className="flex justify-end gap-xs">
                <Button
                  size="sm"
                  onClick={(e) => {
                    e.stopPropagation();
                    openEdit(inst);
                  }}
                >
                  Edit
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={(e) => {
                    e.stopPropagation();
                    openDelete(inst);
                  }}
                  aria-label="Delete instance"
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              </CardFooter>
            </Card>
          ))}
        </div>
      )}

      <Modal open={open} onClose={() => setOpen(false)}>
        <form className="space-y-md" onSubmit={handleSave}>
          <div className="flex items-center gap-sm border-b">
            <button type="button" className={`px-sm py-xs text-sm ${addTab === "local" ? "border-b-2 border-primary text-primary" : "text-muted-foreground"}`} onClick={() => setAddTab("local")}>
              Local instance
            </button>
            <button type="button" className={`px-sm py-xs text-sm ${addTab === "puffer" ? "border-b-2 border-primary text-primary" : "text-muted-foreground"}`} onClick={() => setAddTab("puffer")}>
              From Pufferpanel instance
            </button>
          </div>
          {addTab === "puffer" && hasPuffer && (
            <div className="space-y-xs">
              <label htmlFor="server" className="text-sm font-medium">Server</label>
              {loadingServers ? (
                <p className="text-sm">Loading...</p>
              ) : serverError ? (
                <div className="space-y-xs">
                  <p className="text-sm">{serverError}</p>
                  <Button
                    type="button"
                    onClick={fetchServers}
                    disabled={loadingServers}
                    aria-busy={loadingServers}
                  >
                    Retry
                  </Button>
                </div>
              ) : (
                <Select
                  id="server"
                  value={selectedServer}
                  onChange={(e) => setSelectedServer(e.target.value)}
                  disabled={loadingServers}
                  aria-busy={loadingServers}
                >
                  <option value="">Select a server</option>
                  {servers.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}
                    </option>
                  ))}
                </Select>
              )}
              <div className="space-y-xs">
                <label htmlFor="name" className="text-sm font-medium">Name (optional)</label>
                <Input id="name" value={name} onChange={(e) => setName(e.target.value.replace(/[\p{C}]/gu, ""))} />
                <p className="text-xs text-muted-foreground">If empty, the server name will be used.</p>
              </div>
              {jobProgress && (
                <div className="mt-sm space-y-xs">
                  <p className="text-sm">
                    Sync progress: {jobProgress.processed}/{jobProgress.total} —
                    Succeeded {jobProgress.succeeded}, Failed {jobProgress.failed}
                  </p>
                  {jobProgress.failures?.length > 0 && (
                    <div className="max-h-32 overflow-auto border rounded p-xs text-sm">
                      {jobProgress.failures.map((f, i) => (
                        <div key={i} className="flex justify-between gap-sm">
                          <span className="truncate" title={f.name}>{f.name}</span>
                          <span className="text-destructive truncate" title={f.error}>
                            {f.error}
                          </span>
                        </div>
                      ))}
                    </div>
                  )}
                  {jobProgress.failed > 0 && (
                    <div className="flex gap-sm">
                      <Button
                        type="button"
                        variant="secondary"
                        onClick={async () => {
                          try {
                            await jobs.retry(jobProgress.id);
                            const es = new EventSource(`/api/jobs/${jobProgress.id}/events`);
                            setJobSource(es);
                            es.onmessage = (ev) => {
                              const data = JSON.parse(ev.data);
                              setJobProgress(data);
                              if (
                                data.status === "succeeded" ||
                                data.status === "failed" ||
                                data.status === "canceled"
                              ) {
                                es.close();
                                setJobSource(null);
                              }
                            };
                          } catch (e) {
                            toast.error(
                              e instanceof Error ? e.message : "Failed to retry",
                            );
                          }
                        }}
                      >
                        Retry failed
                      </Button>
                    </div>
                  )}
                </div>
              )}
            </div>
          )}
          {addTab === "puffer" && scanning && !jobProgress && (<p className="text-sm">Starting sync...</p>)}
          {addTab === "local" && (
            <>
              <div className="space-y-xs">
                <label htmlFor="name" className="text-sm font-medium">Name</label>
                <Input id="name" value={name} onChange={(e) => { setName(e.target.value.replace(/[\p{C}]/gu, "")); setNameError(""); }} onBlur={() => { if (!name.trim()) setNameError("Name required"); }} />
                {nameError && (<p className="text-sm text-destructive">{nameError}</p>)}
              </div>
              {!editing && (
                <>
                  <div className="space-y-xs">
                    <label htmlFor="loader" className="text-sm font-medium">Loader</label>
                    <Select id="loader" value={loader} onChange={(e) => setLoader(e.target.value)}>
                      {loaders.map((l) => (<option key={l.id} value={l.id}>{l.label}</option>))}
                    </Select>
                  </div>
                  <div className="flex items-center gap-sm">
                    <Checkbox id="enforce" checked={enforce} onChange={(e) => setEnforce(e.target.checked)} />
                    <label htmlFor="enforce" className="text-sm">Enforce same loader for mods</label>
                  </div>
                </>
              )}
            </>
          )}
          <div className="flex justify-end gap-sm">
            <Button
              type="button"
              variant="secondary"
              onClick={() => setOpen(false)}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={scanning || (addTab === "puffer" ? (loadingServers || !selectedServer) : !name.trim())} aria-busy={scanning}>
              {editing ? "Save" : "Add"}
            </Button>
          </div>
        </form>
      </Modal>

      <Modal open={delState.open} onClose={() => setDelState({ open: false, inst: null })}>
        <form className="space-y-md" onSubmit={handleDelete}>
          <h2 className="text-lg font-medium">Delete instance</h2>
          {delState.inst && (
            <p className="text-sm">Are you sure you want to delete "{delState.inst.name}" and all of its mods?</p>
          )}
          <div className="flex justify-end gap-sm">
            <Button type="button" variant="secondary" onClick={() => setDelState({ open: false, inst: null })}>
              Cancel
            </Button>
            <Button type="submit">Delete</Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
