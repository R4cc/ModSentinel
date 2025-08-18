<script>
  import { createEventDispatcher } from 'svelte';
  import { fade, fly } from 'svelte/transition';

  const dispatch = createEventDispatcher();

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
  let fetchError = false;
  let formError = '';

  const loaderMeta = {
    forge: { icon: '🛠️', desc: 'Forge' },
    fabric: { icon: '🧵', desc: 'Fabric' },
    quilt: { icon: '🪡', desc: 'Quilt' },
    neoforge: { icon: '🔧', desc: 'NeoForge' }
  };

  let gameVersionFilter = '';
  $: filteredGameVersions = gameVersionOptions.filter(gv => gv.toLowerCase().includes(gameVersionFilter.toLowerCase()));

  function badge(gv) {
    if (gameVersionOptions[0] === gv) return 'Latest';
    if (/lts/i.test(gv)) return 'LTS';
    if (/beta|snapshot|pre|rc/i.test(gv)) return 'Beta';
    return '';
  }

  function validate() {
    try {
      new URL(url);
      urlValid = true;
    } catch {
      urlValid = false;
    }
  }

  async function loadMetadata() {
    if (urlLocked) return;
    urlLocked = true;
    loadingMeta = true;
    fetchError = false;
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
    } else {
      urlLocked = false;
      fetchError = true;
    }
  }

  function selectLoader(ld) {
    loader = ld;
    handleLoader();
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
    selectedVersion = versionOptions[0] || null;
  }

  function selectVersion(v) {
    selectedVersion = v;
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
    gameVersionFilter = '';
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
      formError = 'Something went wrong. Please try again.';
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
    <p class="sub">Paste URL → choose loader → version → mod version</p>
  </header>
  <div class="card">
    <form on:submit|preventDefault={addMod} aria-live="polite">
      {#if formError}
        <div class="form-error" role="alert">{formError}</div>
      {/if}
      <section in:fly={{y:16,duration:200,delay:0}}>
        <h3>Enter URL</h3>
        <div class="url-bar">
          <span class="icon" aria-hidden="true">🔗</span>
          <input type="url" bind:value={url} placeholder="Paste URL…" on:input={() => { validate(); urlTouched = true; }} on:blur={validate} aria-invalid={!urlValid && urlTouched} disabled={urlLocked} />
          <button type="button" class="next" on:click={loadMetadata} disabled={!urlValid || urlLocked}>Next</button>
        </div>
        {#if urlTouched && !urlValid}
          <p class="error">Please enter a valid URL.</p>
        {/if}
        {#if fetchError}
          <p class="error state"><span class="tiny">😿</span> Unable to fetch metadata. <button type="button" on:click={loadMetadata}>Retry</button></p>
        {/if}
      </section>

      <section in:fly={{y:16,duration:200,delay:30}}>
        <h3>Select your loader</h3>
        {#if loadingMeta}
          <div class="loader-pills skeleton"></div>
        {:else}
        <fieldset disabled={!metadata} class="loader-pills">
          {#each loaderOptions as ld}
            <button type="button" class="pill {loader === ld ? 'selected' : ''}" on:click={() => selectLoader(ld)}>
              <span class="pill-icon">{loaderMeta[ld]?.icon}</span>
              <span class="pill-desc">{loaderMeta[ld]?.desc || ld}</span>
            </button>
          {/each}
        </fieldset>
        {/if}
      </section>

      <section in:fly={{y:16,duration:200,delay:60}}>
        <h3>Select Minecraft version</h3>
        {#if loadingMeta}
          <div class="skeleton dropdown"></div>
        {:else}
        <fieldset disabled={!(metadata && loader)}>
          <input class="mc-search" type="text" placeholder="Search…" bind:value={gameVersionFilter} disabled={!(metadata && loader)} />
          <select bind:value={game_version} on:change={handleGameVersion} disabled={!(metadata && loader)}>
            {#each filteredGameVersions as gv}
              <option value={gv}>{gv}{badge(gv) ? ` (${badge(gv)})` : ''}</option>
            {/each}
          </select>
        </fieldset>
        {/if}
      </section>

      <section in:fly={{y:16,duration:200,delay:90}}>
        <h3>Select mod version</h3>
        <fieldset disabled={!(metadata && loader && game_version)}>
          {#if versionOptions.length}
            <ul class="version-list">
              {#each versionOptions as v}
                <li>
                  <label>
                    <input type="radio" name="modVersion" value={v.id} checked={selectedVersion === v} on:change={() => selectVersion(v)} disabled={!(metadata && loader && game_version)} />
                    <span class="version">{v.version_number}</span>
                    <span class="date">{(v.date_published || v.date || '').slice(0,10)}</span>
                    <span class="badge {v.version_type}">{v.version_type}</span>
                  </label>
                </li>
              {/each}
            </ul>
          {:else}
            <p class="empty"><span class="tiny">🌌</span> No versions available.</p>
          {/if}
        </fieldset>
      </section>

      <div class="action-bar">
        <button type="submit" class="primary" disabled={!selectedVersion}>Add Entry</button>
        <button type="button" class="secondary" on:click={cancel}>Cancel</button>
      </div>
    </form>
    <aside class="preview">
      <h3>Preview</h3>
      <ul>
        <li class="url-preview">{#if urlValid}<img src={new URL(url).origin + '/favicon.ico'} alt="" class="favicon" />{/if}<span>{url || '-'}</span></li>
        <li>Loader: {loader || '-'}</li>
        <li>MC: {game_version || '-'}</li>
        <li>Version: {selectedVersion ? selectedVersion.version_number : '-'}</li>
      </ul>
    </aside>
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
  .page-header .sub {
    color: var(--color-text-secondary);
    margin: 0;
    font-size: 0.9rem;
  }
  .card {
    background: var(--color-bg-surface);
    border-radius: 14px;
    box-shadow: 0 8px 24px rgba(0,0,0,0.25);
    padding: 1.5rem;
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }
  @media(min-width: 768px) {
    .card {
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
  .url-bar .next {
    border: none;
    border-radius: 999px;
    background: var(--color-orange-accent);
    color: var(--color-bg-page);
    padding: 0 1.25rem;
  }
  .url-bar .next:hover:not(:disabled) {
    box-shadow: 0 0 6px var(--color-orange-accent);
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
  .loader-pills.skeleton {
    height: 2rem;
    background: var(--color-border);
    border-radius: 999px;
    animation: pulse 1.2s infinite ease-in-out;
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
  }
  .pill-icon {
    font-size: 0.9rem;
  }
  .skeleton.dropdown {
    height: 2.5rem;
    background: var(--color-border);
    border-radius: 8px;
    animation: pulse 1.2s infinite ease-in-out;
  }
  .mc-search {
    padding: 0.5rem;
    border: 1px solid var(--color-border);
    border-radius: 8px;
    background: var(--color-bg-page);
    color: var(--color-text-primary);
  }
  select {
    padding: 0.5rem;
    border: 1px solid var(--color-border);
    border-radius: 8px;
    background: var(--color-bg-page);
    color: var(--color-text-primary);
  }
  .version-list {
    list-style: none;
    padding: 0;
    margin: 0;
    max-height: 200px;
    overflow: auto;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .version-list label {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.5rem;
    border: 1px solid var(--color-border);
    border-radius: 8px;
  }
  .version-list .badge {
    margin-left: auto;
    padding: 0.1rem 0.4rem;
    border-radius: 4px;
    font-size: 0.75rem;
    text-transform: uppercase;
  }
  .badge.release { background: var(--color-orange-accent); color: var(--color-bg-page); }
  .badge.beta { background: var(--color-purple-primary); }
  .badge.alpha { background: var(--color-purple-deep); }

  @keyframes pulse {
    0% { opacity: 0.4; }
    50% { opacity: 0.8; }
    100% { opacity: 0.4; }
  }

  .tiny {
    margin-right: 0.25rem;
  }

  .error.state button {
    background: none;
    border: 1px solid var(--color-border);
    color: var(--color-text-primary);
    border-radius: 999px;
    padding: 0.1rem 0.5rem;
    margin-left: 0.25rem;
  }

  .action-bar {
    position: sticky;
    bottom: 0;
    background: var(--color-bg-surface);
    padding-top: 1rem;
    display: flex;
    gap: 1rem;
    justify-content: flex-end;
    border-top: 1px solid var(--color-border);
  }
  .primary {
    background: var(--color-orange-accent);
    border: none;
    color: var(--color-bg-page);
    padding: 0.5rem 1.25rem;
    border-radius: 999px;
  }
  .primary:hover:not(:disabled) {
    box-shadow: 0 0 6px var(--color-orange-accent);
  }
  .primary:active {
    background: var(--color-orange-deep);
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
  .error {
    color: var(--color-orange-accent);
    font-size: 0.875rem;
  }
  .form-error {
    background: var(--color-orange-accent);
    color: var(--color-bg-page);
    padding: 0.5rem 1rem;
    border-radius: 8px;
    margin-bottom: 1rem;
  }
</style>
