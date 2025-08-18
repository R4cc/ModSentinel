<script>
  import { onMount } from 'svelte'

  let mods = []
  let url = ''
  let game_version = ''
  let loader = ''
  let channel = 'release'

  async function loadMods() {
    const res = await fetch('/api/mods')
    mods = await res.json()
  }

  onMount(loadMods)

  async function addMod() {
    const res = await fetch('/api/mods', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url, game_version, loader, channel })
    })
    if (res.ok) {
      mods = await res.json()
      url = ''
      game_version = ''
      loader = ''
      channel = 'release'
    }
  }
</script>

<main>
  <h1>ModSentinel</h1>
  <form class="mod-form" on:submit|preventDefault={addMod}>
    <input bind:value={url} placeholder="Modrinth URL" required />
    <input bind:value={game_version} placeholder="Game Version" required />
    <input bind:value={loader} placeholder="Loader" required />
    <select bind:value={channel}>
      <option value="release">Release</option>
      <option value="beta">Beta</option>
      <option value="alpha">Alpha</option>
    </select>
    <button type="submit">Add</button>
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
    display: grid;
    grid-template-columns: 2fr 1fr 1fr 1fr auto;
    gap: 0.5rem;
    margin-bottom: 2rem;
  }
  .mod-form input,
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
  .mod-form button:hover {
    background-color: #45a049;
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

