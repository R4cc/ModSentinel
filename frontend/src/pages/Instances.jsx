import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Server, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/Button.jsx";
import { Modal } from "@/components/ui/Modal.jsx";
import { Input } from "@/components/ui/Input.jsx";
import { Select } from "@/components/ui/Select.jsx";
import { Checkbox } from "@/components/ui/Checkbox.jsx";
import { Skeleton } from "@/components/ui/Skeleton.jsx";
import {
  Table,
  TableHeader,
  TableRow,
  TableHead,
  TableBody,
  TableCell,
} from "@/components/ui/Table.jsx";
import { EmptyState } from "@/components/ui/EmptyState.jsx";
import {
  getInstances,
  addInstance,
  updateInstance,
  deleteInstance,
  getPufferCreds,
  syncInstances,
  getPufferServers,
} from "@/lib/api.ts";
import { toast } from "sonner";

const loaders = [
  { id: "fabric", label: "Fabric" },
  { id: "forge", label: "Forge" },
  { id: "quilt", label: "Quilt" },
];

export default function Instances() {
  const [instances, setInstances] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState(null);
  const [name, setName] = useState("");
  const [loader, setLoader] = useState(loaders[0].id);
  const [enforce, setEnforce] = useState(true);
  const [hasPuffer, setHasPuffer] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [syncFromPuffer, setSyncFromPuffer] = useState(false);
  const [servers, setServers] = useState([]);
  const [selectedServer, setSelectedServer] = useState("");
  const [loadingServers, setLoadingServers] = useState(false);
  const [scanning, setScanning] = useState(false);
  const navigate = useNavigate();

  useEffect(() => {
    fetchInstances();
  }, []);

  useEffect(() => {
    function check() {
      getPufferCreds()
        .then((c) =>
          setHasPuffer(!!(c.base_url && c.client_id && c.client_secret)),
        )
        .catch(() => setHasPuffer(false));
    }
    check();
    window.addEventListener("pufferpanel-change", check);
    return () => window.removeEventListener("pufferpanel-change", check);
  }, []);

  useEffect(() => {
    if (syncFromPuffer) {
      setLoadingServers(true);
      getPufferServers()
        .then((s) => setServers(s))
        .catch((err) =>
          toast.error(err instanceof Error ? err.message : "Failed to load servers"),
        )
        .finally(() => setLoadingServers(false));
    } else {
      setSelectedServer("");
    }
  }, [syncFromPuffer]);

  async function fetchInstances() {
    setLoading(true);
    setError("");
    try {
      const data = await getInstances();
      setInstances(data);
      if (data.length === 1) {
        navigate(`/instances/${data[0].id}`, { replace: true });
        return;
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load instances");
    } finally {
      setLoading(false);
    }
  }

  async function handleSync() {
    setSyncing(true);
    try {
      await syncInstances();
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
    setLoader(loaders[0].id);
    setEnforce(true);
    setSyncFromPuffer(false);
    setServers([]);
    setSelectedServer("");
    setOpen(true);
  }

  function openEdit(inst) {
    setEditing(inst);
    setName(inst.name);
    setSyncFromPuffer(false);
    setOpen(true);
  }

  async function handleSave(e) {
    e.preventDefault();
    if (!syncFromPuffer && !name.trim()) {
      toast.error("Name required");
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
    if (syncFromPuffer) {
      try {
        setScanning(true);
        const { instance, unmatched, mods } = await syncInstances(selectedServer);
        toast.success("Synced");
        setOpen(false);
        fetchInstances();
        navigate(`/instances/${instance.id}`, { state: { unmatched, mods } });
      } catch (err) {
        toast.error(err instanceof Error ? err.message : "Failed to sync");
      } finally {
        setScanning(false);
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

  const [delState, setDelState] = useState({
    open: false,
    inst: null,
    deleteMods: false,
    targetId: null,
  });

  function openDelete(inst) {
    const others = instances.filter((i) => i.id !== inst.id);
    setDelState({
      open: true,
      inst,
      deleteMods: others.length === 0,
      targetId: others[0]?.id ?? null,
    });
  }

  async function handleDelete(e) {
    e.preventDefault();
    const { inst, deleteMods, targetId } = delState;
    if (!inst) return;
    try {
      await deleteInstance(
        inst.id,
        deleteMods ? undefined : targetId || undefined,
      );
      toast.success("Instance deleted");
      setDelState({
        open: false,
        inst: null,
        deleteMods: false,
        targetId: null,
      });
      fetchInstances();
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Failed to delete instance",
      );
    }
  }

  return (
    <div className="space-y-md">
      <div className="flex justify-end gap-sm">
        {hasPuffer && (
          <Button
            variant="secondary"
            onClick={handleSync}
            disabled={syncing}
            aria-busy={syncing}
          >
            {syncing ? "Syncing..." : "Sync"}
          </Button>
        )}
        <Button onClick={openAdd}>New instance</Button>
      </div>
      {loading && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Loader</TableHead>
              <TableHead>Mods</TableHead>
              <TableHead></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {Array.from({ length: 3 }).map((_, i) => (
              <TableRow key={i}>
                <TableCell>
                  <Skeleton className="h-4 w-32" />
                </TableCell>
                <TableCell>
                  <Skeleton className="h-4 w-20" />
                </TableCell>
                <TableCell>
                  <Skeleton className="h-4 w-8" />
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
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Loader</TableHead>
              <TableHead>Mods</TableHead>
              <TableHead>Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {instances.map((inst) => (
              <TableRow key={inst.id}>
                <TableCell>
                  <Link
                    to={`/instances/${inst.id}`}
                    className="underline hover:no-underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2"
                  >
                    {inst.name}
                  </Link>
                </TableCell>
                <TableCell>{inst.loader}</TableCell>
                <TableCell>{inst.mod_count}</TableCell>
                <TableCell className="flex gap-xs">
                  <Button size="sm" onClick={() => openEdit(inst)}>
                    Edit
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => openDelete(inst)}
                    aria-label="Delete instance"
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <Modal open={open} onClose={() => setOpen(false)}>
        <form className="space-y-md" onSubmit={handleSave}>
          {!editing && (
            <div className="space-y-xs">
              <div className="flex items-center gap-sm">
                <Checkbox
                  id="syncPuffer"
                  checked={syncFromPuffer}
                  disabled={!hasPuffer}
                  onChange={(e) => setSyncFromPuffer(e.target.checked)}
                />
                <label htmlFor="syncPuffer" className="text-sm">
                  Sync from PufferPanel
                </label>
              </div>
              {!hasPuffer && (
                <p className="text-xs text-muted-foreground">
                  Set PufferPanel credentials in Settings
                </p>
              )}
            </div>
          )}
          {syncFromPuffer ? (
            <>
              <div className="space-y-xs">
                <label htmlFor="server" className="text-sm font-medium">
                  Server
                </label>
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
              </div>
              {scanning && <p className="text-sm">Scanning...</p>}
            </>
          ) : (
            <>
              <div className="space-y-xs">
                <label htmlFor="name" className="text-sm font-medium">
                  Name
                </label>
                <Input
                  id="name"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                />
              </div>
              {!editing && (
                <>
                  <div className="space-y-xs">
                    <label htmlFor="loader" className="text-sm font-medium">
                      Loader
                    </label>
                    <Select
                      id="loader"
                      value={loader}
                      onChange={(e) => setLoader(e.target.value)}
                    >
                      {loaders.map((l) => (
                        <option key={l.id} value={l.id}>
                          {l.label}
                        </option>
                      ))}
                    </Select>
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
            <Button
              type="submit"
              disabled={(syncFromPuffer && !selectedServer) || scanning}
              aria-busy={scanning}
            >
              {editing ? "Save" : "Add"}
            </Button>
          </div>
        </form>
      </Modal>

      <Modal
        open={delState.open}
        onClose={() =>
          setDelState({
            open: false,
            inst: null,
            deleteMods: false,
            targetId: null,
          })
        }
      >
        <form className="space-y-md" onSubmit={handleDelete}>
          <h2 className="text-lg font-medium">Delete instance</h2>
          {delState.inst && delState.inst.mod_count > 0 && (
            <p className="text-sm">
              This instance has {delState.inst.mod_count} mods. Choose what to
              do with them.
            </p>
          )}
          {instances.filter((i) => delState.inst && i.id !== delState.inst.id)
            .length > 0 && (
            <div className="flex items-center gap-sm">
              <Checkbox
                id="deleteMods"
                checked={delState.deleteMods}
                onChange={(e) =>
                  setDelState((s) => ({ ...s, deleteMods: e.target.checked }))
                }
              />
              <label htmlFor="deleteMods" className="text-sm">
                Delete contained mods
              </label>
            </div>
          )}
          {!delState.deleteMods &&
            instances.filter((i) => delState.inst && i.id !== delState.inst.id)
              .length > 0 && (
              <div className="space-y-xs">
                <label htmlFor="target" className="text-sm font-medium">
                  Move mods to
                </label>
                <Select
                  id="target"
                  value={delState.targetId ?? ""}
                  onChange={(e) =>
                    setDelState((s) => ({
                      ...s,
                      targetId: Number(e.target.value),
                    }))
                  }
                >
                  {instances
                    .filter((i) => delState.inst && i.id !== delState.inst.id)
                    .map((i) => (
                      <option key={i.id} value={i.id}>
                        {i.name}
                      </option>
                    ))}
                </Select>
              </div>
            )}
          <div className="flex justify-end gap-sm">
            <Button
              type="button"
              variant="secondary"
              onClick={() =>
                setDelState({
                  open: false,
                  inst: null,
                  deleteMods: false,
                  targetId: null,
                })
              }
            >
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={!delState.deleteMods && !delState.targetId}
            >
              Delete
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
