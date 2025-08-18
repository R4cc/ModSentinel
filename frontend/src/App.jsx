import { useEffect, useState } from 'react';
import AddEntry from './AddEntry.jsx';

export default function App() {
  const [mods, setMods] = useState([]);
  const [editingMod, setEditingMod] = useState(null);
  const [deleteTarget, setDeleteTarget] = useState(null);

  async function loadMods() {
    const res = await fetch('/api/mods');
    setMods(await res.json());
  }

  useEffect(() => {
    loadMods();
  }, []);

  async function confirmDelete() {
    if (!deleteTarget) return;
    await fetch(`/api/mods/${deleteTarget.id}`, { method: 'DELETE' });
    setDeleteTarget(null);
    loadMods();
  }

  return (
    <main>
      <h1>ModSentinel</h1>
      <div className="content">
        <AddEntry onAdded={loadMods} editingMod={editingMod} onEditDone={() => setEditingMod(null)} />
        <section className="mods-section">
          <h2>Tracked Mods</h2>
          <div className="table-card">
            {mods.length ? (
              <table className="mod-table">
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
                    <th>Download</th>
                    <th>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {mods.map(mod => (
                    <tr key={mod.id}>
                      <td><img src={mod.icon_url} alt={mod.name} className="icon" /></td>
                      <td>{mod.name}</td>
                      <td><a href={mod.url} target="_blank" rel="noreferrer">link</a></td>
                      <td>{mod.loader}</td>
                      <td>{mod.game_version}</td>
                      <td>
                        {mod.current_version === mod.available_version ? (
                          mod.current_version
                        ) : (
                          `${mod.current_version} → ${mod.available_version}`
                        )}
                      </td>
                      <td>
                        {mod.channel === mod.available_channel ? (
                          mod.channel
                        ) : (
                          `${mod.channel} → ${mod.available_channel}`
                        )}
                      </td>
                      <td>
                        {mod.current_version === mod.available_version && mod.channel === mod.available_channel ? (
                          <span className="status up-to-date">Up to date</span>
                        ) : (
                          <span className="status update-available">Update available</span>
                        )}
                      </td>
                      <td>
                        {(mod.current_version !== mod.available_version || mod.channel !== mod.available_channel) && (
                          <a className="download" href={mod.download_url} target="_blank" rel="noreferrer">Download</a>
                        )}
                      </td>
                      <td>
                        <div className="actions">
                          <button type="button" onClick={() => setEditingMod(mod)}>Edit</button>
                          <button type="button" className="delete" onClick={() => setDeleteTarget(mod)}>Delete</button>
                        </div>
                      </td>
                  </tr>
                  ))}
                </tbody>
              </table>
            ) : (
              <p className="empty-state">Nothing tracked yet. Paste a Modrinth or CurseForge link to get started. For example, try <a href="https://modrinth.com/mod/distant-horizons" target="_blank" rel="noreferrer">Distant Horizons</a>.</p>
            )}
          </div>
        </section>
      </div>
      {deleteTarget && (
        <div className="modal-overlay">
          <div className="modal">
            <p>Delete {deleteTarget.name}?</p>
            <div className="buttons">
              <button type="button" className="secondary" onClick={() => setDeleteTarget(null)}>Cancel</button>
              <button type="button" className="primary" onClick={confirmDelete}>Delete</button>
            </div>
          </div>
        </div>
      )}
    </main>
  );
}
