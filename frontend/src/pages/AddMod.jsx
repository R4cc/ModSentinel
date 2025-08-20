import { useEffect, useRef, useState } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { toast } from 'sonner';
import { Box, Cog, Gauge } from 'lucide-react';
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/Card.jsx';
import { Button } from '@/components/ui/Button.jsx';
import { Input } from '@/components/ui/Input.jsx';
import { Checkbox } from '@/components/ui/Checkbox.jsx';
import { Badge } from '@/components/ui/Badge.jsx';
import { Skeleton } from '@/components/ui/Skeleton.jsx';
import { cn } from '@/lib/utils.js';
import { addMod, getToken } from '@/lib/api.ts';
import { useNavigate } from 'react-router-dom';
import { useAddModStore, initialState } from '@/stores/addModStore.js';

const steps = ['Mod URL', 'Loader', 'Minecraft Version', 'Mod Version'];
const loaders = [
  {
    id: 'fabric',
    label: 'Fabric',
    description: 'Lightweight mod loader',
    icon: Box,
  },
  {
    id: 'forge',
    label: 'Forge',
    description: 'Classic mod loader',
    icon: Cog,
  },
  {
    id: 'quilt',
    label: 'Quilt',
    description: 'Fork of Fabric with patches',
    icon: Gauge,
  },
];

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

  const [hasToken, setHasToken] = useState(true);
  useEffect(() => {
    getToken()
      .then((t) => setHasToken(!!t))
      .catch(() => setHasToken(false));
  }, []);

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
    if (!fresh) {
      resetWizard();
    }
  }, [resetWizard]);

  useEffect(() => {
    if (step === 3 && refs[3].current) {
      refs[3].current.focus();
    }
  }, [safeModVersions.length, step]);

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

  const navigate = useNavigate();

  async function handleAdd() {
    const selected = safeModVersions.find((v) => v.id === selectedModVersion);
    if (!selected) return;
    try {
      await addMod({
        url,
        loader,
        game_version: mcVersion,
        channel: selected.version_type,
      });
      toast.success('Mod added');
      resetWizard();
      navigate('/mods');
    } catch (err) {
      if (err instanceof Error && err.message === 'token required') {
        toast.error('Modrinth token required');
      } else {
        toast.error('Failed to add mod');
      }
    }
  }

  const filteredModVersions = includePre
    ? safeModVersions
    : safeModVersions.filter((v) => v.version_type === 'release');

  if (!hasToken) {
    return (
      <div className="p-md">
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
                    'h-1 rounded-full bg-muted',
                    i <= step && 'bg-primary'
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
                <label className="block text-sm font-medium" htmlFor="url">
                  Mod URL
                </label>
                <Input
                  id="url"
                  ref={refs[0]}
                  value={url}
                  onChange={(e) => setUrl(e.target.value)}
                  onBlur={(e) => validateUrl(e.target.value)}
                  placeholder="https://..."
                />
                <p className="text-sm text-muted-foreground">
                  Paste the mod page URL from Modrinth.
                </p>
                {urlError && (
                  <p className="text-sm text-red-600" role="alert">
                    {urlError}
                  </p>
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
                <fieldset className="space-y-xs">
                  <legend className="text-sm font-medium">Choose loader</legend>
                  {loaders.map((l, idx) => {
                    const Icon = l.icon;
                    return (
                      <label
                        key={l.id}
                        className={cn(
                          'flex cursor-pointer items-center gap-sm rounded-md border p-sm',
                          loader === l.id && 'border-primary'
                        )}
                      >
                        <input
                          ref={idx === 0 ? refs[1] : null}
                          type="radio"
                          name="loader"
                          value={l.id}
                          className="h-4 w-4"
                          onChange={() => setLoader(l.id)}
                          checked={loader === l.id}
                        />
                        <Icon className="h-4 w-4" />
                        <div className="flex flex-col">
                          <span className="font-medium">{l.label}</span>
                          <span className="text-sm text-muted-foreground">
                            {l.description}
                          </span>
                        </div>
                      </label>
                    );
                  })}
                </fieldset>
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
                <label className="block text-sm font-medium" htmlFor="mc-version">
                  Minecraft version
                </label>
                {loadingVersions ? (
                  <Skeleton className="h-10 w-full" />
                ) : (
                  <>
                    <Input
                      list="mc-versions"
                      id="mc-version"
                      ref={refs[2]}
                      value={mcVersion}
                      onChange={(e) => setMcVersion(e.target.value)}
                      placeholder="Search versions"
                    />
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
                          'flex w-full items-center justify-between rounded-md border p-sm text-left',
                          selectedModVersion === v.id && 'border-primary'
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
