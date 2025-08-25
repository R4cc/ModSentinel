import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Server, Trash2 } from "lucide-react";
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
  const [nameError, setNameError] = useState("");
  const [loader, setLoader] = useState(loaders[0].id);
  const [enforce, setEnforce] = useState(true);
  const [hasToken, setHasToken] = useState(true);
  const [hasPuffer, setHasPuffer] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [servers, setServers] = useState([]);
  const [selectedServer, setSelectedServer] = useState("");
  const [loadingServers, setLoadingServers] = useState(false);
  const [serverError, setServerError] = useState("");
  const [scanning, setScanning] = useState(false);
  const navigate = useNavigate();

  useEffect(() => {
    fetchInstances();
  }, []);

  useEffect(() => {
    function check() {
      getSecretStatus("pufferpanel")
        .then((s) => setHasPuffer(s.exists))
        .catch(() => setHasPuffer(false));
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
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load instances");
    } finally {
      setLoading(false);
    }
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
    setOpen(true);
    if (hasPuffer) fetchServers();
  }

  function openEdit(inst) {
    setEditing(inst);
    setName(inst.name);
    setNameError("");
    setEnforce(inst.enforce_same_loader);
    setSelectedServer(inst.pufferpanel_server_id || "");
    setServerError("");
    setOpen(true);
    if (hasPuffer) fetchServers();
  }

  async function handleSave(e) {
    e.preventDefault();
    if (!selectedServer && !name.trim()) {
      toast.error("Name required");
      return;
    }
    if (selectedServer) {
      try {
        setScanning(true);
        let res;
        if (editing) {
          res = await syncInstances(selectedServer, editing.id);
        } else {
          const created = await addInstance({
            name: "",
            loader: "",
            enforce_same_loader: true,
            pufferpanel_server_id: selectedServer,
          });
          res = await syncInstances(selectedServer, created.id);
        }
        toast.success("Synced");
        setOpen(false);
        fetchInstances();
        navigate(`/instances/${res.instance.id}`, {
          state: { unmatched: res.unmatched, mods: res.mods },
        });
      } catch (err) {
        toast.error(err instanceof Error ? err.message : "Failed to sync");
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
      <div className="min-h-5 space-y-xs">
        {!hasToken && (
          <p className="text-sm text-muted-foreground">
            Set a Modrinth token in Settings to enable update checks.
          </p>
        )}
        {!hasPuffer && (
          <p className="text-sm text-muted-foreground">
            Set PufferPanel credentials in Settings to enable sync.
          </p>
        )}
      </div>
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
        <Button onClick={openAdd}>Add instance</Button>
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
              className="flex flex-col justify-between cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2"
            >
              <CardHeader>
                <CardTitle>{inst.name}</CardTitle>
              </CardHeader>
              <CardContent className="flex items-center justify-between text-sm">
                <span className="capitalize">{inst.loader}</span>
                <span>{inst.mod_count} mods</span>
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
          {hasPuffer && (
            <div className="space-y-xs">
              <label htmlFor="server" className="text-sm font-medium">
                Server
              </label>
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
                  <option value="">None</option>
                  {servers.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}
                    </option>
                  ))}
                </Select>
              )}
            </div>
          )}
          {selectedServer ? (
            scanning && <p className="text-sm">Scanning...</p>
          ) : (
            <>
              <div className="space-y-xs">
                <label htmlFor="name" className="text-sm font-medium">
                  Name
                </label>
                <Input
                  id="name"
                  value={name}
                  onChange={(e) => {
                    setName(e.target.value.replace(/[\p{C}]/gu, ""));
                    setNameError("");
                  }}
                  onBlur={() => {
                    if (!name.trim()) setNameError("Name required");
                  }}
                />
                {nameError && (
                  <p className="text-sm text-destructive">{nameError}</p>
                )}
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
              disabled={
                scanning || loadingServers || (!selectedServer && !name.trim())
              }
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
