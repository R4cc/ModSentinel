import { useState, useEffect } from 'react';
import './AddEntry.css';

const loaderMeta = {
  forge: { icon: '🛠️', desc: 'Forge' },
  fabric: { icon: '🧵', desc: 'Fabric' },
  quilt: { icon: '🪡', desc: 'Quilt' },
  neoforge: { icon: '🔧', desc: 'NeoForge' }
};

export default function AddEntry({ onAdded, editingMod, onEditDone }) {
  const steps = ['URL', 'Loader', 'MC version', 'Mod version', 'Confirm'];
  const [step, setStep] = useState(0);

  const [url, setUrl] = useState('');
  const [urlValid, setUrlValid] = useState(false);
  const [urlTouched, setUrlTouched] = useState(false);
  const [urlLocked, setUrlLocked] = useState(false);

  const [loader, setLoader] = useState('');
  const [gameVersion, setGameVersion] = useState('');
  const [selectedVersion, setSelectedVersion] = useState(null);

  const [metadata, setMetadata] = useState(null);
  const [loaderOptions, setLoaderOptions] = useState([]);
  const [gameVersionOptions, setGameVersionOptions] = useState([]);
  const [versionOptions, setVersionOptions] = useState([]);

  const [loadingMeta, setLoadingMeta] = useState(false);
  const [fetchErrorMsg, setFetchErrorMsg] = useState('');
  const [formError, setFormError] = useState('');

  const [channelFilter, setChannelFilter] = useState('release');
  const filteredVersions = versionOptions.filter(v => channelFilter ? v.version_type === channelFilter : true);

  const [toastMsg, setToastMsg] = useState('');
  const [showToastFlag, setShowToastFlag] = useState(false);
  const [editingId, setEditingId] = useState(null);

  function showToast(msg) {
    setToastMsg(msg);
    setShowToastFlag(true);
    setTimeout(() => setShowToastFlag(false), 3500);
  }

  useEffect(() => {
    if (editingMod) {
      (async () => {
        setEditingId(editingMod.id);
        setUrl(editingMod.url);
        setUrlValid(true);
        setUrlTouched(true);
        setUrlLocked(false);
        setChannelFilter(editingMod.channel);
        setLoadingMeta(true);
        const res = await fetch('/api/mods/metadata', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ url: editingMod.url })
        });
        setLoadingMeta(false);
        if (res.ok) {
          const data = await res.json();
          setMetadata(data);
          setLoaderOptions(data.loaders || []);
          setLoader(editingMod.loader);
          const gvSet = new Set();
          data.versions.forEach(v => {
            if (v.loaders.includes(editingMod.loader)) {
              v.game_versions.forEach(gv => gvSet.add(gv));
            }
          });
          const gvOptions = Array.from(gvSet).sort((a, b) => b.localeCompare(a));
          setGameVersionOptions(gvOptions);
          setGameVersion(editingMod.game_version);
          const versions = data.versions
            .filter(v => v.loaders.includes(editingMod.loader) && v.game_versions.includes(editingMod.game_version))
            .sort((a, b) => new Date(b.date_published || b.date || 0) - new Date(a.date_published || a.date || 0));
          setVersionOptions(versions);
          const sel = versions.find(v => v.version_number === editingMod.current_version && v.version_type === editingMod.channel);
          setSelectedVersion(sel || versions[0] || null);
          setStep(4);
        } else {
          setFetchErrorMsg(`Couldn't fetch mod data (HTTP ${res.status})`);
        }
      })();
    }
  }, [editingMod]);

  function validate(value = url) {
    try {
      new URL(value);
      setUrlValid(true);
    } catch {
      setUrlValid(false);
    }
  }

  async function loadMetadata() {
    if (urlLocked || !urlValid) return;
    setUrlLocked(true);
    setLoadingMeta(true);
    setFetchErrorMsg('');
    const res = await fetch('/api/mods/metadata', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url })
    });
    setLoadingMeta(false);
    if (res.ok) {
      const data = await res.json();
      setMetadata(data);
      setLoaderOptions(data.loaders || []);
      setLoader('');
      setGameVersion('');
      setSelectedVersion(null);
      setGameVersionOptions([]);
      setVersionOptions([]);
      setStep(1);
    } else {
      setUrlLocked(false);
      setFetchErrorMsg(`Couldn't fetch mod data (HTTP ${res.status})`);
    }
  }

  function handleLoader(ld = loader) {
    if (!metadata || !ld) return;
    const set = new Set();
    metadata.versions.forEach(v => {
      if (v.loaders.includes(ld)) {
        v.game_versions.forEach(gv => set.add(gv));
      }
    });
    const options = Array.from(set).sort((a,b) => b.localeCompare(a));
    setGameVersionOptions(options);
    setGameVersion('');
    setVersionOptions([]);
    setSelectedVersion(null);
  }

  function selectLoader(ld) {
    setLoader(ld);
    handleLoader(ld);
  }

  function handleGameVersion(gv = gameVersion, ld = loader) {
    if (!metadata || !ld || !gv) return;
    const versions = metadata.versions
      .filter(v => v.loaders.includes(ld) && v.game_versions.includes(gv))
      .sort((a,b) => new Date(b.date_published || b.date || 0) - new Date(a.date_published || a.date || 0));
    setVersionOptions(versions);
    setSelectedVersion(versions[0] || null);
    setChannelFilter(versions[0]?.version_type || '');
  }

  function selectVersion(v) {
    setSelectedVersion(v);
  }

  function cancel() {
    setUrl('');
    setUrlValid(false);
    setUrlTouched(false);
    setUrlLocked(false);
    setLoader('');
    setGameVersion('');
    setSelectedVersion(null);
    setMetadata(null);
    setLoaderOptions([]);
    setGameVersionOptions([]);
    setVersionOptions([]);
    setChannelFilter('release');
    setLoadingMeta(false);
    setFetchErrorMsg('');
    setFormError('');
    setStep(0);
    setShowToastFlag(false);
    setEditingId(null);
    onEditDone && onEditDone();
  }

  async function addMod(e) {
    e.preventDefault();
    if (!selectedVersion) return;
    const channel = selectedVersion.version_type;
    const endpoint = editingId ? `/api/mods/${editingId}` : '/api/mods';
    const method = editingId ? 'PUT' : 'POST';
    const res = await fetch(endpoint, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url, game_version: gameVersion, loader, channel })
    });
    if (res.ok) {
      onAdded && onAdded();
      showToast(editingId ? 'Entry updated' : 'Entry added');
      cancel();
      setFormError('');
    } else {
      setFormError(`Failed to ${editingId ? 'update' : 'add'} mod.`);
    }
  }

  return (
    <div className="add-entry">
      <header className="page-header">
        <h2>{editingId ? 'Edit Entry' : 'Add Entry'}</h2>
      </header>

      <div className="card">
        <div className="stepper">
          {steps.map((label, i) => (
            <button
              key={label}
              type="button"
              className={i === step ? 'active' : i < step ? 'done' : 'disabled'}
              onClick={() => { if (i < step) setStep(i); }}
              disabled={i > step}
            >
              {i < step ? '✓ ' : ''}{label}
            </button>
          ))}
        </div>

        <form onSubmit={addMod} aria-live="polite">
          {formError && <div className="form-error" role="alert">{formError}</div>}

          {step === 0 && (
            <section>
              <h3>Enter URL</h3>
              <div className="url-bar">
                <input
                  type="url"
                  value={url}
                  placeholder="Paste URL"
                  onChange={e => { const val = e.target.value; setUrl(val); validate(val); }}
                  onKeyDown={e => { if (e.key === 'Enter') { validate(); loadMetadata(); } }}
                  onPaste={e => {
                    const pasted = e.clipboardData.getData('text');
                    validate(pasted);
                    setTimeout(loadMetadata, 0);
                  }}
                  onBlur={() => { setUrlTouched(true); validate(); }}
                  aria-invalid={!urlValid && urlTouched}
                  disabled={urlLocked}
                />
                {fetchErrorMsg && (
                  <button type="button" className="retry" onClick={loadMetadata} aria-label="Retry">↻</button>
                )}
              </div>
              {urlTouched && !urlValid && <p className="error">URL must be Modrinth/CurseForge.</p>}
              {fetchErrorMsg && <p className="error state"><span className="tiny">😿</span> {fetchErrorMsg}</p>}
              {loadingMeta && <p className="status"><span className="spinner" aria-hidden="true"></span> Fetching mod info…</p>}
              <button type="button" className="continue" onClick={loadMetadata} disabled={!urlValid || loadingMeta}>Continue</button>
            </section>
          )}

          {step === 1 && (
            <section>
              <h3>Select your loader</h3>
              <fieldset className="loader-pills">
                {Object.keys(loaderMeta).map(ld => (
                  <button
                    type="button"
                    key={ld}
                    className={`pill ${loader === ld ? 'selected' : ''}`}
                    disabled={!loaderOptions.includes(ld)}
                    title={!loaderOptions.includes(ld) ? `No files for ${loaderMeta[ld].desc}` : ''}
                    onClick={() => selectLoader(ld)}
                  >
                    <span className="pill-icon">{loaderMeta[ld].icon}</span>
                    <span className="pill-desc">{loaderMeta[ld].desc}</span>
                  </button>
                ))}
              </fieldset>
              <button
                type="button"
                className="continue"
                onClick={() => setStep(2)}
                disabled={!loader}
              >
                Continue
              </button>
            </section>
          )}

          {step === 2 && (
            <section>
              <h3>Select Minecraft version</h3>
              <fieldset>
                <input
                  list="mc-versions"
                  value={gameVersion}
                  onChange={e => setGameVersion(e.target.value)}
                  placeholder="Search versions"
                />
                <datalist id="mc-versions">
                  {gameVersionOptions.map(gv => (
                    <option key={gv} value={gv} />
                  ))}
                </datalist>
              </fieldset>
              <button
                type="button"
                className="continue"
                onClick={() => { handleGameVersion(); setStep(3); }}
                disabled={!gameVersion}
              >
                Continue
              </button>
            </section>
          )}

          {step === 3 && (
            <section>
              <h3>Select mod version</h3>
              <div className="chips">
                {['release','beta','alpha'].map(c => (
                  <button
                    type="button"
                    key={c}
                    className={`chip ${channelFilter === c ? 'active' : ''}`}
                    onClick={() => setChannelFilter(c)}
                    disabled={!versionOptions.some(v => v.version_type === c)}
                    title={!versionOptions.some(v => v.version_type === c) ? 'No version available' : ''}
                  >
                    {c[0].toUpperCase() + c.slice(1)}
                  </button>
                ))}
              </div>
              <fieldset>
                {filteredVersions.length ? (
                    <table className="version-table">
                      <thead>
                        <tr><th></th><th>Version</th><th>Release channel</th><th>Published</th><th>File size</th></tr>
                      </thead>
                      <tbody>
                        {filteredVersions.map(v => (
                          <tr key={v.id}>
                            <td data-label="Select"><input type="radio" name="modVersion" value={v.id} checked={selectedVersion === v} onChange={() => selectVersion(v)} /></td>
                            <td data-label="Version">{v.version_number}</td>
                            <td data-label="Release channel"><span className={`badge ${v.version_type}`} title={v.version_type !== 'release' ? 'Pre-release build; may be unstable.' : ''}>{v.version_type}</span></td>
                            <td data-label="Published">{(v.date_published || v.date || '').slice(0,10)}</td>
                            <td data-label="File size">{(v.files && v.files[0] && (v.files[0].size/1024/1024).toFixed(2)) || '-'} MB</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                ) : (
                  <p className="empty"><span className="tiny">🌌</span> No versions available.</p>
                )}
              </fieldset>
              <button
                type="button"
                className="continue"
                onClick={() => setStep(4)}
                disabled={!selectedVersion}
              >
                Continue
              </button>
            </section>
          )}

          {step === 4 && (
            <>
              <section className="confirm">
                <h3>Confirm</h3>
                <p>Loader: {loader} • MC: {gameVersion} • Selected build: {selectedVersion.version_number} ({selectedVersion.version_type.toUpperCase()})</p>
              </section>
              <div className="action-bar">
                <div className="summary">{metadata?.title || ''} • {loader} • MC {gameVersion} • {selectedVersion.version_number}</div>
                <ul className="checklist">
                  <li>{metadata ? '✔' : '✖'} URL</li>
                  <li>{loader ? '✔' : '✖'} Loader</li>
                  <li>{gameVersion ? '✔' : '✖'} MC</li>
                  <li>{selectedVersion ? '✔' : '✖'} Mod version</li>
                </ul>
                <button type="submit" className="primary" disabled={!selectedVersion}>{editingId ? 'Update Entry' : 'Add Entry'}</button>
                <button type="button" className="secondary" onClick={cancel}>Cancel</button>
              </div>
            </>
          )}
        </form>
      </div>

      {(metadata || loadingMeta) && (
        <aside className="preview">
          {loadingMeta ? (
            <div className="skeleton preview-skel"></div>
          ) : (
            <>
              <h3>Preview</h3>
              <ul>
                <li className="url-preview">{urlValid && <img src={new URL(url).origin + '/favicon.ico'} alt="" className="favicon" />}<span>{url || '-'}</span></li>
                <li>Loader: {loader || '-'}</li>
                <li>MC: {gameVersion || '-'}</li>
                <li>Selected build: {selectedVersion ? `${selectedVersion.version_number} (${selectedVersion.version_type.toUpperCase()})` : '-'}</li>
              </ul>
            </>
          )}
        </aside>
      )}

      {showToastFlag && <div className="toast" role="status">{toastMsg}</div>}
    </div>
  );
}
