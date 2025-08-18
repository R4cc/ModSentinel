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
  <div class="content">
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
  <div class="mod-table-container">
    {#if mods.length}
      <table class="mod-table">
        <thead>
          <tr>
            <th></th>
            <th>Name</th>
            <th>URL</th>
            <th>Loader</th>
            <th>MC Version</th>
            <th>Version</th>
            <th>Release</th>
            <th>Status</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
        {#each mods as mod}
          <tr>
            <td><img src={mod.icon_url} alt="{mod.name}" class="icon" /></td>
            <td>{mod.name}</td>
            <td><a href={mod.url} target="_blank" rel="noreferrer">link</a></td>
            <td>{mod.loader}</td>
            <td>{mod.game_version}</td>
            <td>
              {#if mod.current_version === mod.available_version}
                {mod.current_version}
              {:else}
                {mod.current_version} &rarr; {mod.available_version}
              {/if}
            </td>
            <td>
              {#if mod.channel === mod.available_channel}
                {mod.channel}
              {:else}
                {mod.channel} &rarr; {mod.available_channel}
              {/if}
            </td>
            <td>
              {#if mod.current_version === mod.available_version && mod.channel === mod.available_channel}
                <span class="status up-to-date">Up to date</span>
              {:else}
                <span class="status update-available">Update available</span>
              {/if}
            </td>
            <td>
              {#if mod.current_version !== mod.available_version || mod.channel !== mod.available_channel}
                <a class="download" href={mod.download_url} target="_blank" rel="noreferrer">Download</a>
              {/if}
            </td>
          </tr>
        {/each}
        </tbody>
      </table>
    {:else}
      <p>No mods tracked.</p>
    {/if}
  </div>
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
  .content {
    display: flex;
    flex-direction: column;
    gap: 2rem;
  }
  .mod-table {
    width: 100%;
    border-collapse: collapse;
  }
  .mod-table th,
  .mod-table td {
    padding: 0.5rem;
    border-bottom: 1px solid #ddd;
  }
  .mod-table th {
    background-color: #f0f0f0;
  }
  .icon {
    width: 32px;
    height: 32px;
  }
  .status {
    padding: 0.25rem 0.5rem;
    border-radius: 4px;
    font-size: 0.875rem;
  }
  .status.up-to-date {
    background-color: #c8e6c9;
    color: #256029;
  }
  .status.update-available {
    background-color: #ffe0b2;
    color: #8a6d3b;
  }
  .download {
    background-color: #3498db;
    color: #fff;
    padding: 0.25rem 0.5rem;
    border-radius: 4px;
    text-decoration: none;
  }
  @media (min-width: 768px) {
    .content {
      flex-direction: row;
      align-items: flex-start;
    }
    .mod-form {
      width: 40%;
    }
    .mod-table-container {
      width: 60%;
    }
  }
</style>

