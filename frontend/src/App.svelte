<script>
  import { onMount } from 'svelte'

  let mods = []
  let url = ''
  let loader = ''
  let game_version = ''
  let channel = ''
  let metadata = null
  let loaderOptions = []
  let gameVersionOptions = []
  let channelOptions = []

  async function loadMods() {
    const res = await fetch('/api/mods')
    mods = await res.json()
  }

  onMount(loadMods)

  async function loadMetadata() {
    const res = await fetch('/api/mods/metadata', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url })
    })
    if (res.ok) {
      metadata = await res.json()
      loaderOptions = metadata.loaders
      loader = ''
      game_version = ''
      channel = ''
      gameVersionOptions = []
      channelOptions = []
    }
  }

  function handleLoader() {
    if (!metadata || !loader) return
    const set = new Set()
    metadata.versions.forEach(v => {
      if (v.loaders.includes(loader)) {
        v.game_versions.forEach(gv => set.add(gv))
      }
    })
    gameVersionOptions = Array.from(set).sort((a, b) => b.localeCompare(a))
    game_version = gameVersionOptions[0] || ''
    handleGameVersion()
  }

  function handleGameVersion() {
    if (!metadata || !loader || !game_version) return
    const set = new Set()
    metadata.versions.forEach(v => {
      if (v.loaders.includes(loader) && v.game_versions.includes(game_version)) {
        set.add(v.version_type)
      }
    })
    const order = ['release', 'beta', 'alpha']
    channelOptions = Array.from(set).sort((a, b) => order.indexOf(a) - order.indexOf(b))
    channel = channelOptions[0] || ''
  }

  async function addMod() {
    const res = await fetch('/api/mods', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url, game_version, loader, channel })
    })
    if (res.ok) {
      mods = await res.json()
      url = ''
      loader = ''
      game_version = ''
      channel = ''
      metadata = null
      loaderOptions = []
      gameVersionOptions = []
      channelOptions = []
    }
  }
</script>

<main>
  <h1>ModSentinel</h1>
  <form class="mod-form" on:submit|preventDefault={addMod}>
    <div class="url-step">
      <input class="search-bar" bind:value={url} placeholder="Modrinth URL" required />
      <button type="button" on:click={loadMetadata} disabled={!url}>Next</button>
    </div>
    {#if metadata}
      <div class="step">
        <p>Select your loader</p>
        <select bind:value={loader} on:change={handleLoader} required>
          <option value="" disabled selected>Select loader</option>
          {#each loaderOptions as ld}
            <option value={ld}>{ld}</option>
          {/each}
        </select>
      </div>
    {/if}
    {#if metadata && loader}
      <div class="step">
        <p>Select your Minecraft version</p>
        <select bind:value={game_version} on:change={handleGameVersion} required>
          {#each gameVersionOptions as gv}
            <option value={gv}>{gv}</option>
          {/each}
        </select>
      </div>
    {/if}
    {#if metadata && loader && game_version}
      <div class="step">
        <p>Select mod version</p>
        <select bind:value={channel} required>
          {#each channelOptions as ch}
            <option value={ch}>{ch}</option>
          {/each}
        </select>
        <button type="submit">Add</button>
      </div>
    {/if}
  </form>
  <div class="mod-list">
    {#if mods.length}
      {#each mods as mod}
        <div class="mod-item">
          <a href={mod.URL} target="_blank" rel="noreferrer">{mod.URL}</a>
          <span>{mod.GameVersion} {mod.Loader}</span>
          <span class="version">{mod.LatestVersion}</span>
        </div>
      {/each}
    {:else}
      <p>No mods tracked.</p>
    {/if}
  </div>
</main>

<style>
  main {
    max-width: 800px;
    margin: 0 auto;
    padding: 2rem;
    font-family: system-ui, sans-serif;
  }
  h1 {
    text-align: center;
    color: #2c3e50;
  }
  .mod-form {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 1rem;
    margin-bottom: 2rem;
  }
  .url-step {
    display: flex;
    justify-content: center;
    width: 100%;
    gap: 0.5rem;
  }
  .url-step .search-bar {
    flex: 1;
    padding: 0.5rem;
    border: 1px solid #ccc;
    border-radius: 4px;
  }
  .mod-form select,
  .mod-form button {
    padding: 0.5rem;
    border: 1px solid #ccc;
    border-radius: 4px;
  }
  .mod-form button {
    background-color: #4caf50;
    color: white;
    cursor: pointer;
    transition: background-color 0.3s;
  }
  .mod-form button:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }
  .mod-form button:hover:enabled {
    background-color: #45a049;
  }
  .step {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.5rem;
  }
  .mod-list {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }
  .mod-item {
    padding: 0.75rem;
    border-radius: 4px;
    background-color: #f5f5f5;
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .mod-item a {
    color: #3498db;
    text-decoration: none;
  }
  .mod-item .version {
    font-weight: bold;
  }
</style>

