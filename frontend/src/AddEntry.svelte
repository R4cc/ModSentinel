<script>
  import { createEventDispatcher } from 'svelte';

  const dispatch = createEventDispatcher();

  // stepper
  let step = 0;
  const steps = ['URL', 'Loader', 'MC version', 'Mod version', 'Confirm'];

  // form state
  let url = '';
  let urlValid = false;
  let urlTouched = false;
  let urlLocked = false;

  let loader = '';
  let game_version = '';
  let selectedVersion = null;

  let metadata = null;
  let loaderOptions = [];
  let gameVersionOptions = [];
  let versionOptions = [];
  let loadingMeta = false;
  let fetchErrorMsg = '';
  let formError = '';

  const loaderMeta = {
    forge: { icon: '🛠️', desc: 'Forge' },
    fabric: { icon: '🧵', desc: 'Fabric' },
    quilt: { icon: '🪡', desc: 'Quilt' },
    neoforge: { icon: '🔧', desc: 'NeoForge' }
  };

  function validate() {
    try {
      new URL(url);
      urlValid = true;
    } catch {
      urlValid = false;
    }
  }

  async function loadMetadata() {
    if (urlLocked || !urlValid) return;
    urlLocked = true;
    loadingMeta = true;
    fetchErrorMsg = '';
    const res = await fetch('/api/mods/metadata', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url })
    });
    loadingMeta = false;
    if (res.ok) {
      metadata = await res.json();
      loaderOptions = metadata.loaders || [];
      loader = '';
      game_version = '';
      selectedVersion = null;
      gameVersionOptions = [];
      versionOptions = [];
      step = 1;
    } else {
      urlLocked = false;
      fetchErrorMsg = `Couldn't fetch mod data (HTTP ${res.status})`;
    }
  }

  function selectLoader(ld) {
    loader = ld;
    handleLoader();
    step = 2;
  }

  function handleLoader() {
    if (!metadata || !loader) return;
    const set = new Set();
    metadata.versions.forEach(v => {
      if (v.loaders.includes(loader)) {
        v.game_versions.forEach(gv => set.add(gv));
      }
    });
    gameVersionOptions = Array.from(set).sort((a, b) => b.localeCompare(a));
    game_version = gameVersionOptions[0] || '';
    handleGameVersion();
  }

  function handleGameVersion() {
    if (!metadata || !loader || !game_version) return;
    const versions = metadata.versions
      .filter(v => v.loaders.includes(loader) && v.game_versions.includes(game_version))
      .sort((a, b) => new Date(b.date_published || b.date || 0) - new Date(a.date_published || a.date || 0));
    versionOptions = versions;
    selectedVersion = versions[0] || null;
    step = 3;
  }

  let channelFilter = 'release';
  $: filteredVersions = versionOptions.filter(v => channelFilter ? v.version_type === channelFilter : true);

  function selectVersion(v) {
    selectedVersion = v;
    step = 4;
  }

  function cancel() {
    url = '';
    urlValid = false;
    urlTouched = false;
    urlLocked = false;
    loader = '';
    game_version = '';
    selectedVersion = null;
    metadata = null;
    loaderOptions = [];
    gameVersionOptions = [];
    versionOptions = [];
    channelFilter = 'release';
    step = 0;
  }

  async function addMod() {
    if (!selectedVersion) return;
    const channel = selectedVersion.version_type;
    const res = await fetch('/api/mods', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url, game_version, loader, channel })
    });
    if (res.ok) {
      dispatch('added');
      showToast('Entry added');
      cancel();
      formError = '';
    } else {
      formError = 'Failed to add mod.';
    }
  }

  // toast
  let toastMsg = '';
  let showToastFlag = false;
  function showToast(msg) {
    toastMsg = msg;
    showToastFlag = true;
    setTimeout(() => showToastFlag = false, 3500);
  }
</script>

<div class="add-entry">
  <header class="page-header">
    <h2>Add Entry</h2>
  </header>

  <div class="card {metadata ? 'with-preview' : ''}">
    <div class="stepper">
      {#each steps as label, i}
        <button type="button"
          class="{i === step ? 'active' : i < step ? 'done' : 'disabled'}"
          on:click={() => { if (i < step) step = i; }}
          disabled={i > step}>{i < step ? '✓ ' : ''}{label}</button>
      {/each}
    </div>

    <form on:submit|preventDefault={addMod} aria-live="polite">
      {#if formError}
        <div class="form-error" role="alert">{formError}</div>
      {/if}

      {#if step === 0}
        <section>
          <h3>Enter URL</h3>
          <div class="url-bar">
            <span class="icon" aria-hidden="true">🔗</span>
            <input type="url" bind:value={url} placeholder="Paste URL" on:input={() => { validate(); urlTouched = true; }}
              on:keydown={(e) => { if (e.key === 'Enter') { validate(); loadMetadata(); } }}
              on:paste={() => { setTimeout(() => { validate(); loadMetadata(); }, 0); }}
              on:blur={validate} aria-invalid={!urlValid && urlTouched} disabled={urlLocked} />
            {#if fetchErrorMsg}
              <button type="button" class="retry" on:click={loadMetadata} aria-label="Retry">↻</button>
            {/if}
          </div>
          {#if urlTouched && !urlValid}
            <p class="error">URL must be Modrinth/CurseForge.</p>
          {/if}
          {#if fetchErrorMsg}
            <p class="error state"><span class="tiny">😿</span> {fetchErrorMsg}</p>
          {/if}
          {#if loadingMeta}
            <p class="status"><span class="spinner" aria-hidden="true"></span> Fetching mod info…</p>
          {/if}
          <button type="button" class="continue" on:click={loadMetadata} disabled={!urlValid || loadingMeta}>Continue</button>
          <small class="help"><a href="/docs/supported-links">Supported links</a></small>
        </section>
      {:else if step === 1}
        <section>
          <h3>Select your loader</h3>
          <fieldset class="loader-pills">
            {#each Object.keys(loaderMeta) as ld}
              <button type="button"
                class="pill {loader === ld ? 'selected' : ''}"
                disabled={!loaderOptions.includes(ld)}
                title={!loaderOptions.includes(ld) ? `No files for ${loaderMeta[ld].desc}` : ''}
                on:click={() => selectLoader(ld)}>
                <span class="pill-icon">{loaderMeta[ld].icon}</span>
                <span class="pill-desc">{loaderMeta[ld].desc}</span>
              </button>
            {/each}
          </fieldset>
        </section>
      {:else if step === 2}
        <section>
          <h3>Select Minecraft version</h3>
          <fieldset>
            <select bind:value={game_version} on:change={handleGameVersion}>
              {#each gameVersionOptions as gv}
                <option value={gv}>{gv}</option>
              {/each}
            </select>
          </fieldset>
        </section>
      {:else if step === 3}
        <section>
          <h3>Select mod version</h3>
          <div class="chips">
            {#each ['release','beta','alpha'] as c}
              <button type="button" class="chip {channelFilter === c ? 'active' : ''}" on:click={() => channelFilter = c}>{c[0].toUpperCase() + c.slice(1)}</button>
            {/each}
          </div>
          <fieldset>
            {#if filteredVersions.length}
              <table class="version-table">
                <thead>
                  <tr><th></th><th>Version</th><th>Release channel</th><th>Published</th><th>File size</th></tr>
                </thead>
                <tbody>
                  {#each filteredVersions as v}
                    <tr>
                      <td><input type="radio" name="modVersion" value={v.id} checked={selectedVersion === v} on:change={() => selectVersion(v)} /></td>
                      <td>{v.version_number}</td>
                      <td><span class="badge {v.version_type}" title={v.version_type !== 'release' ? 'Pre-release build; may be unstable.' : ''}>{v.version_type}</span></td>
                      <td>{(v.date_published || v.date || '').slice(0,10)}</td>
                      <td>{(v.files && v.files[0] && (v.files[0].size/1024/1024).toFixed(2)) || '-'} MB</td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            {:else}
              <p class="empty"><span class="tiny">🌌</span> No versions available.</p>
            {/if}
          </fieldset>
        </section>
      {:else if step === 4}
        <section class="confirm">
          <h3>Confirm</h3>
          <p>Loader: {loader} • MC: {game_version} • Selected build: {selectedVersion.version_number} ({selectedVersion.version_type.toUpperCase()})</p>
        </section>
        <div class="action-bar">
          <div class="summary">{metadata?.title || ''} • {loader} • MC {game_version} • {selectedVersion.version_number}</div>
          <ul class="checklist">
            <li>{metadata ? '✔' : '✖'} URL</li>
            <li>{loader ? '✔' : '✖'} Loader</li>
            <li>{game_version ? '✔' : '✖'} MC</li>
            <li>{selectedVersion ? '✔' : '✖'} Mod version</li>
          </ul>
          <button type="submit" class="primary" disabled={!selectedVersion}>Add Entry</button>
          <button type="button" class="secondary" on:click={cancel}>Cancel</button>
        </div>
      {/if}
    </form>

    {#if metadata || loadingMeta}
      <aside class="preview">
        {#if loadingMeta}
          <div class="skeleton preview-skel"></div>
        {:else}
          <h3>Preview</h3>
          <ul>
            <li class="url-preview">{#if urlValid}<img src={new URL(url).origin + '/favicon.ico'} alt="" class="favicon" />{/if}<span>{url || '-'}</span></li>
            <li>Loader: {loader || '-'}</li>
            <li>MC: {game_version || '-'}</li>
            <li>Selected build: {selectedVersion ? `${selectedVersion.version_number} (${selectedVersion.version_type.toUpperCase()})` : '-'}</li>
          </ul>
        {/if}
      </aside>
    {/if}
  </div>

  {#if showToastFlag}
    <div class="toast" role="status">{toastMsg}</div>
  {/if}
</div>

<style>
  .add-entry {
    color: var(--color-text-primary);
  }
  .page-header {
    text-align: left;
    margin-bottom: 1rem;
  }
  .page-header h2 {
    color: var(--color-purple-deep);
    margin: 0;
  }
  .card {
    background: var(--color-bg-surface);
    border-radius: 14px;
    box-shadow: 0 8px 24px rgba(0,0,0,0.25);
    padding: 1.5rem;
    display: flex;
    flex-direction: column;
    gap: 1rem;
    max-width: 560px;
    margin: 0 auto;
  }
  .card.with-preview {
    max-width: none;
  }
  @media(min-width: 768px) {
    .card.with-preview {
      flex-direction: row;
      align-items: flex-start;
    }
    form {
      flex: 1;
    }
    .preview {
      width: 340px;
      margin-left: 1.5rem;
    }
  }
  .stepper {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .stepper button {
    border: none;
    background: var(--color-bg-page);
    border-radius: 999px;
    padding: 0.25rem 0.75rem;
    color: var(--color-text-secondary);
  }
  .stepper button.done {
    color: var(--color-text-primary);
  }
  .stepper button.active {
    background: var(--color-purple-primary);
    color: var(--color-bg-page);
  }
  .stepper button:disabled {
    opacity: 0.4;
  }
  section {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    margin-bottom: 1rem;
  }
  h3 {
    margin: 0;
    color: var(--color-purple-deep);
    font-size: 1rem;
  }
  .url-bar {
    display: flex;
    gap: 0.5rem;
    align-items: center;
  }
  .url-bar .icon {
    padding: 0 0 0 0.5rem;
  }
  .url-bar input {
    flex: 1;
    padding: 0.75rem 1rem;
    border-radius: 999px;
    border: 1px solid var(--color-border);
    background: var(--color-bg-page);
    color: var(--color-text-primary);
  }
  .url-bar .retry {
    border: none;
    background: transparent;
    font-size: 1rem;
    cursor: pointer;
  }
  .continue {
    background: var(--color-orange-accent);
    color: var(--color-bg-page);
    border: none;
    padding: 0.5rem 1.25rem;
    border-radius: 999px;
    align-self: flex-start;
  }
  .continue:disabled {
    opacity: 0.5;
  }
  fieldset {
    border: none;
    padding: 0;
    margin: 0;
  }
  .loader-pills {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
  }
  .pill {
    padding: 0.5rem 1rem;
    border-radius: 999px;
    border: 1px solid var(--color-border);
    background: var(--color-bg-page);
    color: var(--color-text-primary);
    display: flex;
    align-items: center;
    gap: 0.25rem;
  }
  .pill.selected {
    background: var(--color-purple-primary);
    border-color: var(--color-purple-primary);
    color: var(--color-bg-page);
  }
  .pill:disabled {
    opacity: 0.4;
    cursor: default;
  }
  select {
    padding: 0.5rem;
    border: 1px solid var(--color-border);
    border-radius: 8px;
    background: var(--color-bg-page);
    color: var(--color-text-primary);
  }
  .chips {
    display: flex;
    gap: 0.5rem;
    margin-bottom: 0.5rem;
  }
  .chip {
    border: 1px solid var(--color-border);
    background: var(--color-bg-page);
    color: var(--color-text-primary);
    padding: 0.25rem 0.75rem;
    border-radius: 999px;
  }
  .chip.active {
    background: var(--color-purple-primary);
    border-color: var(--color-purple-primary);
    color: var(--color-bg-page);
  }
  .version-table {
    width: 100%;
    border-collapse: collapse;
  }
  .version-table th,
  .version-table td {
    padding: 0.5rem;
    border-bottom: 1px solid var(--color-border);
    text-align: left;
  }
  .badge {
    padding: 0.1rem 0.4rem;
    border-radius: 4px;
    font-size: 0.75rem;
    text-transform: uppercase;
  }
  .badge.release { background: var(--color-orange-accent); color: var(--color-bg-page); }
  .badge.beta { background: var(--color-purple-primary); }
  .badge.alpha { background: var(--color-purple-deep); }
  .tiny {
    margin-right: 0.25rem;
  }
  .error {
    color: var(--color-orange-accent);
    font-size: 0.875rem;
  }
  .status {
    font-size: 0.875rem;
  }
  .spinner {
    display: inline-block;
    width: 1rem;
    height: 1rem;
    border: 2px solid var(--color-border);
    border-top-color: var(--color-purple-primary);
    border-radius: 50%;
    animation: spin 1s linear infinite;
    margin-right: 0.5rem;
  }
  @keyframes spin {
    to { transform: rotate(360deg); }
  }
  .action-bar {
    position: sticky;
    bottom: 0;
    background: var(--color-bg-surface);
    padding-top: 1rem;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    border-top: 1px solid var(--color-border);
  }
  @media(min-width: 480px) {
    .action-bar {
      flex-direction: row;
      align-items: center;
      justify-content: space-between;
    }
  }
  .summary {
    font-size: 0.875rem;
  }
  .checklist {
    list-style: none;
    padding: 0;
    margin: 0;
    display: flex;
    gap: 0.5rem;
    font-size: 0.875rem;
  }
  .checklist li {
    white-space: nowrap;
  }
  .primary {
    background: var(--color-orange-accent);
    border: none;
    color: var(--color-bg-page);
    padding: 0.5rem 1.25rem;
    border-radius: 999px;
  }
  .primary:disabled {
    opacity: 0.5;
  }
  .secondary {
    background: transparent;
    border: 1px solid var(--color-border);
    color: var(--color-text-primary);
    padding: 0.5rem 1.25rem;
    border-radius: 999px;
  }
  .preview {
    border: 1px solid var(--color-border);
    border-radius: 8px;
    padding: 1rem;
  }
  .preview ul {
    list-style: none;
    padding: 0;
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .preview .favicon {
    width: 16px;
    height: 16px;
    margin-right: 0.25rem;
  }
  .preview-skel {
    height: 120px;
    background: var(--color-border);
    border-radius: 8px;
    animation: pulse 1.2s infinite ease-in-out;
  }
  @keyframes pulse {
    0% { opacity: 0.4; }
    50% { opacity: 0.8; }
    100% { opacity: 0.4; }
  }
  .form-error {
    background: var(--color-orange-accent);
    color: var(--color-bg-page);
    padding: 0.5rem 1rem;
    border-radius: 8px;
    margin-bottom: 1rem;
  }
  .toast {
    position: fixed;
    right: 1rem;
    bottom: 1rem;
    background: var(--color-bg-surface);
    border: 1px solid var(--color-border);
    padding: 0.75rem 1rem;
    border-radius: 8px;
    box-shadow: 0 4px 12px rgba(0,0,0,0.3);
  }
  a { color: var(--color-purple-primary); }
</style>

