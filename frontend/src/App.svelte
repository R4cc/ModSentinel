<script>
  import { onMount } from 'svelte';
  import AddEntry from './AddEntry.svelte';

  let mods = [];

  async function loadMods() {
    const res = await fetch('/api/mods');
    mods = await res.json();
  }

  onMount(loadMods);
</script>

<main>
  <h1>ModSentinel</h1>
  <div class="content">
    <AddEntry on:added={loadMods} />
    <section class="mods-section">
      <h2>Tracked Mods</h2>
      <div class="table-card">
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
        <p class="empty-state">Nothing tracked yet. Paste a Modrinth or CurseForge link to get started. For example, try <a href="https://modrinth.com/mod/distant-horizons" target="_blank" rel="noreferrer">Distant Horizons</a>.</p>
      {/if}
      </div>
    </section>
  </div>
</main>

<style>
  main {
    max-width: 800px;
    margin: 0 auto;
    padding: 2rem;
  }
  h1 {
    text-align: center;
    color: var(--color-purple-deep);
  }
  .content {
    display: flex;
    flex-direction: column;
    gap: 2rem;
  }
  .mods-section h2 {
    color: var(--color-purple-deep);
    margin: 0 0 0.5rem 0;
  }
  .table-card {
    background: var(--color-bg-surface);
    border-radius: 14px;
    box-shadow: 0 8px 24px rgba(0,0,0,0.25);
    padding: 1.5rem;
  }
  .mod-table {
    width: 100%;
    border-collapse: collapse;
  }
  .mod-table th,
  .mod-table td {
    padding: 0.5rem;
    border-bottom: 1px solid var(--color-border);
  }
  .mod-table th {
    background-color: var(--color-bg-surface);
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
    background-color: #2b8a3e;
    color: #fff;
  }
  .status.update-available {
    background-color: #b58900;
    color: #fff;
  }
  .download {
    background-color: var(--color-purple-primary);
    color: #fff;
    padding: 0.25rem 0.5rem;
    border-radius: 4px;
    text-decoration: none;
  }
  .empty-state {
    color: var(--color-text-secondary);
  }
</style>
