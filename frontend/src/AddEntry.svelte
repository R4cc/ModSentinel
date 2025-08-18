<script>
  import { createEventDispatcher } from 'svelte';
  import { fade, slide } from 'svelte/transition';

  const dispatch = createEventDispatcher();

  let url = '';
  let urlValid = false;
  let urlTouched = false;

  let loader = '';
  let game_version = '';
  let selectedVersion = null;

  let metadata = null;
  let loaderOptions = [];
  let gameVersionOptions = [];
  let versionOptions = [];

  let gameVersionFilter = '';
  $: filteredGameVersions = gameVersionOptions.filter(gv => gv.toLowerCase().includes(gameVersionFilter.toLowerCase()));

  function validate() {
    try {
      new URL(url);
      urlValid = true;
    } catch {
      urlValid = false;
    }
  }

  async function loadMetadata() {
    const res = await fetch('/api/mods/metadata', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url })
    });
    if (res.ok) {
      metadata = await res.json();
      loaderOptions = metadata.loaders || [];
      loader = '';
      game_version = '';
      selectedVersion = null;
      gameVersionOptions = [];
      versionOptions = [];
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
      <section>
        <h3>Enter URL</h3>
        <div class="url-bar">
          <input type="url" bind:value={url} placeholder="Paste URL…" on:input={() => { validate(); urlTouched = true; }} on:blur={validate} aria-invalid={!urlValid && urlTouched} />
          <button type="button" class="next" on:click={loadMetadata} disabled={!urlValid}>Next</button>
        </div>
        {#if urlTouched && !urlValid}
          <p class="error">Please enter a valid URL.</p>
        {/if}
      </section>

      {#if metadata}
        <section in:slide={{duration:200}}>
          <h3>Select your loader</h3>
          <div class="loader-pills">
            {#each loaderOptions as ld}
              <button type="button" class="pill {loader === ld ? 'selected' : ''}" on:click={() => selectLoader(ld)}>{ld}</button>
            {/each}
          </div>
        </section>
      {/if}

      {#if metadata && loader}
        <section in:slide={{duration:200}}>
          <h3>Select Minecraft version</h3>
          <input class="mc-search" type="text" placeholder="Search…" bind:value={gameVersionFilter} />
          <select bind:value={game_version} on:change={handleGameVersion}>
            {#each filteredGameVersions as gv}
              <option value={gv}>{gv}</option>
            {/each}
          </select>
        </section>
      {/if}

      {#if metadata && loader && game_version}
        <section in:slide={{duration:200}}>
          <h3>Select mod version</h3>
          {#if versionOptions.length}
            <ul class="version-list">
              {#each versionOptions as v}
                <li>
                  <label>
                    <input type="radio" name="modVersion" value={v.id} checked={selectedVersion === v} on:change={() => selectVersion(v)} />
                    <span class="version">{v.version_number}</span>
                    <span class="date">{(v.date_published || v.date || '').slice(0,10)}</span>
                    <span class="badge {v.version_type}">{v.version_type}</span>
                  </label>
                </li>
              {/each}
            </ul>
          {:else}
            <p class="empty">No versions available.</p>
          {/if}
        </section>
      {/if}

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
    text-align: center;
    margin-bottom: 1rem;
  }
  .page-header h2 {
    color: var(--color-purple-primary);
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
    color: var(--color-purple-primary);
    font-size: 1rem;
  }
  .url-bar {
    display: flex;
    gap: 0.5rem;
  }
  .url-bar input {
    flex: 1;
    padding: 0.75rem 1rem;
    border-radius: 999px;
    border: 1px solid var(--color-border);
    background: var(--color-bg-page);
    color: var(--color-text-primary);
  }
  .url-bar input:focus {
    outline: 2px solid var(--color-purple-primary);
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
  }
  .pill.selected {
    background: var(--color-purple-primary);
    border-color: var(--color-purple-primary);
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
</style>
