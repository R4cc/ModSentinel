import { useEffect, useRef, useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { toast } from "@/lib/toast.ts";
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/Card.jsx";
import { Button } from "@/components/ui/Button.jsx";
import { Input } from "@/components/ui/Input.jsx";
import { Select } from "@/components/ui/Select.jsx";
import { Checkbox } from "@/components/ui/Checkbox.jsx";
import { Badge } from "@/components/ui/Badge.jsx";
import { Skeleton } from "@/components/ui/Skeleton.jsx";
import { cn } from "@/lib/utils.js";
import { addMod, getInstance, getSecretStatus, searchMods } from "@/lib/api.ts";
import { useNavigate, useParams, useLocation, Link } from "react-router-dom";
import { ArrowLeft, X } from "lucide-react";
import ModIcon from "@/components/ModIcon.jsx";
import { useAddModStore, initialState } from "@/stores/addModStore.js";
import { useMetaStore } from "@/stores/metaStore.js";
import LoaderSelect from "@/components/LoaderSelect.jsx";
import { parseJarFilename } from "@/lib/jar.ts";

const steps = ["Select Mod", "Loader", "Minecraft Version", "Mod Version"];

export default function AddMod() {
  const {
    step,
    url,
    urlError,
    loader,
    mcVersion,
    versions,
    loadingVersions,
    modVersions,
    loadingModVersions,
    includePre,
    selectedModVersion,
    nonce,
    setUrl,
    validateUrl,
    setLoader,
    setMcVersion,
    setIncludePre,
    setSelectedModVersion,
    nextStep,
    prevStep,
    fetchVersions,
    fetchModVersions,
    resetWizard,
  } = useAddModStore();

  const safeVersions = versions ?? [];
  const safeModVersions = modVersions ?? [];

  const refs = [useRef(null), useRef(null), useRef(null), useRef(null)];
  const didAutoSkipLoader = useRef(false);
  const [query, setQuery] = useState("");
  const [searching, setSearching] = useState(false);
  const [results, setResults] = useState([]);
  const [selectedHit, setSelectedHit] = useState(null);
  const searchTimer = useRef(null);

  const navigate = useNavigate();
  const { id } = useParams();
  const instanceId = Number(id);
  const location = useLocation();
  const unresolvedFile = location.state?.file;

  const [hasToken, setHasToken] = useState(true);
  useEffect(() => {
    getSecretStatus("modrinth")
      .then((s) => setHasToken(s.exists))
      .catch(() => setHasToken(false));
  }, []);

  const [instance, setInstance] = useState(null);
  const [useServerVersion, setUseServerVersion] = useState(false);
  useEffect(() => {
    getInstance(instanceId)
      .then((data) => {
        setInstance(data);
        setLoader(data.loader);
        if (data.gameVersion) {
          setUseServerVersion(true);
          // initialize mcVersion to server-detected value if empty
          setMcVersion((v) => v || data.gameVersion || "");
        } else {
          setUseServerVersion(false);
        }
      })
      .catch((err) =>
        toast.error(
          err instanceof Error ? err.message : "Failed to load instance",
        ),
      );
  }, [instanceId, setLoader]);

  useEffect(() => {
    refs[step]?.current?.focus();
  }, [step]);

  useEffect(() => {
    const state = useAddModStore.getState();
    const init = initialState();
    const fresh =
      state.step === init.step &&
      state.url === init.url &&
      state.loader === init.loader &&
      state.mcVersion === init.mcVersion &&
      state.selectedModVersion === init.selectedModVersion &&
      (state.versions ?? []).length === 0 &&
      (state.modVersions ?? []).length === 0;
    if (!fresh && !unresolvedFile) {
      resetWizard();
    }
  }, [resetWizard, unresolvedFile]);

  useEffect(() => {
    if (unresolvedFile) {
      const { slug } = parseJarFilename(unresolvedFile);
      if (slug) setUrl(`https://modrinth.com/mod/${slug}`);
    }
  }, [unresolvedFile, setUrl]);

  // live search
  useEffect(() => {
    if (step !== 0) return;
    if (searchTimer.current) clearTimeout(searchTimer.current);
    const q = query.trim();
    if (!q) {
      setResults([]);
      return;
    }
    // If a full Modrinth URL is pasted, search by slug part
    let effective = q;
    try {
      const u = new URL(q);
      const parts = u.pathname.split("/");
      const idx = parts.findIndex((p) => ["mod","plugin","datapack","resourcepack"].includes(p));
      if (idx !== -1 && parts[idx+1]) effective = parts[idx+1];
    } catch {}
    searchTimer.current = setTimeout(async () => {
      setSearching(true);
      try {
        const hits = await searchMods(effective);
        setResults(hits || []);
      } catch (err) {
        setResults([]);
        if (err instanceof Error && err.message === "token required") {
          toast.error("Modrinth token required");
        }
      } finally {
        setSearching(false);
      }
    }, 250);
    return () => {
      if (searchTimer.current) clearTimeout(searchTimer.current);
    };
  }, [query, step]);

  useEffect(() => {
    if (step === 3 && refs[3].current) {
      refs[3].current.focus();
    }
  }, [safeModVersions.length, step]);

  // Removed enforce-same-loader auto-skip; loader step is always available
  useEffect(() => {
    /* no-op */
  }, [step]);

  // Reset auto-skip guard when instance changes
  useEffect(() => {
    didAutoSkipLoader.current = false;
  }, [instanceId]);

  useEffect(() => {
    if (step === 2 && safeVersions.length === 0) {
      fetchVersions();
    }
  }, [step, safeVersions.length, fetchVersions]);

  useEffect(() => {
    if (step === 3) {
      fetchModVersions();
    }
  }, [step, url, loader, mcVersion, fetchModVersions]);

  const nextDisabled = [
    !url || !!urlError,
    !loader,
    !mcVersion,
    !selectedModVersion,
  ][step];

  function handleNext() {
    if (step === 0) {
      const ok = validateUrl(url);
      if (!ok) return;
    }
    if (!nextDisabled) nextStep();
  }

  async function handleAdd() {
    const selected = safeModVersions.find((v) => v.id === selectedModVersion);
    if (!selected) return;
    try {
      const resp = await addMod({
        url,
        loader,
        game_version: mcVersion,
        channel: selected.version_type,
        version_id: selected.id,
        instance_id: instanceId,
      });
      if (resp?.warning) {
        toast.warning(resp.warning);
      }
      toast.success("Mod added");
      resetWizard();
      navigate(`/instances/${instanceId}`, {
        state: { mods: resp.mods, resolved: unresolvedFile },
      });
    } catch (err) {
      if (err instanceof Error && err.message === "token required") {
        toast.error("Modrinth token required");
      } else if (err instanceof Error) {
        toast.error(err.message);
      } else {
        toast.error("Failed to add mod");
      }
    }
  }

  const filteredModVersions = includePre
    ? safeModVersions
    : safeModVersions.filter((v) => v.version_type === "release");

  if (!hasToken) {
    return (
      <div className="p-md">
        <Link
          to="/instances"
          className="mb-md inline-flex items-center gap-xs text-sm text-muted-foreground hover:underline"
        >
          <ArrowLeft className="h-4 w-4" aria-hidden="true" />
          Back to Instances
        </Link>
        <Card className="mx-auto w-full max-w-md">
          <CardHeader>
            <CardTitle>Add Mod</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground">
              Modrinth token required. Add your token in settings.
            </p>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="p-md" key={nonce}>
      <Link
        to="/instances"
        className="mb-md inline-flex items-center gap-xs text-sm text-muted-foreground hover:underline"
      >
        <ArrowLeft className="h-4 w-4" aria-hidden="true" />
        Back to Instances
      </Link>
      <Card className="mx-auto w-full max-w-md">
        <CardHeader>
          <CardTitle>Add Mod</CardTitle>
        </CardHeader>
        <CardContent className="space-y-md">
          <ol className="flex items-center gap-sm" aria-label="Progress">
            {steps.map((s, i) => (
              <li key={s} className="flex-1">
                <div
                  className={cn(
                    "h-1 rounded-full bg-muted",
                    i <= step && "bg-primary",
                  )}
                />
              </li>
            ))}
          </ol>

          <AnimatePresence mode="wait">
            {step === 0 && (
              <motion.div
                key={0}
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                className="space-y-sm"
              >
                {!selectedHit ? (
                  <>
                    <label className="block text-sm font-medium" htmlFor="mod-search">
                      Search Modrinth
                    </label>
                    <Input
                      id="mod-search"
                      ref={refs[0]}
                      value={query}
                      onChange={(e) => setQuery(e.target.value)}
                      placeholder="Type a mod name or paste a Modrinth link"
                    />
                    <div className="space-y-xs">
                      {searching ? (
                        <>
                          <Skeleton className="h-16 w-full" />
                          <Skeleton className="h-16 w-full" />
                          <Skeleton className="h-16 w-full" />
                        </>
                      ) : (
                        results.length > 0 && (
                          <div className="max-h-60 overflow-y-auto space-y-xs">
                            {results.map((h) => (
                              <button
                                type="button"
                                key={h.slug}
                                onClick={() => {
                                  const u = `https://modrinth.com/mod/${h.slug}`;
                                  setUrl(u);
                                  validateUrl(u);
                                  setSelectedHit(h);
                                }}
                                className="flex w-full items-center gap-sm rounded-md border p-sm text-left hover:border-primary"
                              >
                                <ModIcon url={h.icon_url} cacheKey={h.slug} />
                                <div className="min-w-0">
                                  <div className="font-medium truncate">{h.title}</div>
                                  {h.description && (
                                    <div className="text-sm text-muted-foreground truncate">
                                      {h.description}
                                    </div>
                                  )}
                                </div>
                              </button>
                            ))}
                          </div>
                        )
                      )}
                    </div>
                  </>
                ) : (
                  <div className="relative rounded-md border p-sm">
                    <button
                      type="button"
                      aria-label="Clear selection"
                      className="absolute right-sm top-sm text-muted-foreground hover:text-foreground"
                      onClick={() => {
                        setSelectedHit(null);
                        setUrl("");
                        setQuery("");
                      }}
                    >
                      <X className="h-4 w-4" />
                    </button>
                    <div className="flex items-center gap-sm">
                      <ModIcon url={selectedHit.icon_url} cacheKey={selectedHit.slug} />
                      <div className="min-w-0">
                        <div className="font-medium truncate">{selectedHit.title}</div>
                        {selectedHit.description && (
                          <div className="text-sm text-muted-foreground truncate">
                            {selectedHit.description}
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                )}
              </motion.div>
            )}

            {step === 1 && (
              <motion.div
                key={1}
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                className="space-y-sm"
              >
                <div className="space-y-xs">
                  <label className="text-sm font-medium">Loader</label>
                  <LoaderSelect loaders={metaLoaders} value={loader} onChange={setLoader} disabled={!metaLoaded || metaLoaders.length === 0} />
                </div>
              </motion.div>
            )}

            {step === 2 && (
              <motion.div
                key={2}
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                className="space-y-sm"
              >
                <label
                  className="block text-sm font-medium"
                  htmlFor="mc-version"
                >
                  Minecraft version
                </label>
                {loadingVersions ? (
                  <Skeleton className="h-10 w-full" />
                ) : (
                  <>
                    {instance?.gameVersion && useServerVersion ? (
                      <div className="flex items-center justify-between gap-sm rounded-md border p-sm">
                        <div className="text-sm">
                          <div className="text-muted-foreground">Using server's version (from PufferPanel)</div>
                          <div className="font-medium">{instance.gameVersion}</div>
                        </div>
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => setUseServerVersion(false)}
                        >
                          Switch to manual
                        </Button>
                      </div>
                    ) : (
                      <div className="space-y-xs">
                        {instance?.gameVersion && (
                          <div className="flex items-center justify-between gap-sm rounded-md border p-xs text-sm">
                            <span>
                              Use server's version: <span className="font-medium">{instance.gameVersion}</span>
                            </span>
                            <Button
                              size="sm"
                              variant="secondary"
                              onClick={() => {
                                setUseServerVersion(true);
                                setMcVersion(instance.gameVersion || "");
                              }}
                            >
                              Use this
                            </Button>
                          </div>
                        )}
                        <Input
                          list="mc-versions"
                          id="mc-version"
                          ref={refs[2]}
                          value={mcVersion}
                          onChange={(e) => setMcVersion(e.target.value)}
                          placeholder="Search versions"
                          disabled={!!(instance?.gameVersion && useServerVersion)}
                        />
                      </div>
                    )}
                    <datalist id="mc-versions">
                      {safeVersions.map((v) => (
                        <option key={v} value={v} />
                      ))}
                    </datalist>
                  </>
                )}
              </motion.div>
            )}

            {step === 3 && (
              <motion.div
                key={3}
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                className="space-y-sm"
              >
                <div className="flex items-center gap-sm">
                  <Checkbox
                    id="pre"
                    checked={includePre}
                    onChange={(e) => setIncludePre(e.target.checked)}
                  />
                  <label htmlFor="pre" className="text-sm">
                    Show prereleases
                  </label>
                </div>
                {loadingModVersions ? (
                  <div className="space-y-xs">
                    <Skeleton className="h-14 w-full" />
                    <Skeleton className="h-14 w-full" />
                    <Skeleton className="h-14 w-full" />
                  </div>
                ) : (
                  <div className="space-y-xs">
                    {filteredModVersions.map((v, idx) => (
                      <button
                        key={v.id}
                        ref={idx === 0 ? refs[3] : null}
                        onClick={() => setSelectedModVersion(v.id)}
                        className={cn(
                          "flex w-full items-center justify-between rounded-md border p-sm text-left",
                          selectedModVersion === v.id && "border-primary",
                        )}
                      >
                        <div>
                          <div className="font-medium">{v.version_number}</div>
                          <div className="text-sm text-muted-foreground">
                            {new Date(v.date_published).toLocaleString()}
                          </div>
                        </div>
                        <Badge>{v.version_type}</Badge>
                      </button>
                    ))}
                  </div>
                )}
              </motion.div>
            )}
          </AnimatePresence>
        </CardContent>
        <CardFooter className="flex justify-between">
          <Button variant="secondary" onClick={prevStep} disabled={step === 0}>
            Back
          </Button>
          {step === 3 ? (
            <Button onClick={handleAdd} disabled={nextDisabled}>
              Add
            </Button>
          ) : (
            <Button onClick={handleNext} disabled={nextDisabled}>
              Next
            </Button>
          )}
        </CardFooter>
      </Card>
    </div>
  );
}
  const metaLoaders = useMetaStore((s) => s.loaders);
  const metaLoaded = useMetaStore((s) => s.loaded);
  const loadMeta = useMetaStore((s) => s.load);
  useEffect(() => { if (!metaLoaded) loadMeta(); }, [metaLoaded, loadMeta]);
