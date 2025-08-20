import { render, screen, fireEvent, act } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { describe, it, expect, beforeEach, vi } from 'vitest';

vi.mock('@/lib/api.ts', () => ({
  getToken: vi.fn().mockResolvedValue('token'),
  addMod: vi.fn().mockResolvedValue(undefined),
}));

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import AddMod from './AddMod.jsx';
import { useAddModStore, initialState } from '@/stores/addModStore.js';
import { useOpenAddMod } from '@/hooks/useOpenAddMod.js';

describe('AddMod page', () => {
  beforeEach(() => {
    useAddModStore.setState({
      ...initialState(),
      nonce: 0,
      fetchVersions: vi.fn(),
      fetchModVersions: vi.fn(),
    });
  });

  it('resets wizard state when reopened after adding a mod', async () => {
    const Mods = () => {
      const openAddMod = useOpenAddMod();
      return <button onClick={openAddMod}>Add Another</button>;
    };

    render(
      <MemoryRouter initialEntries={['/mods/add']}>
        <Routes>
          <Route path='/mods/add' element={<AddMod />} />
          <Route path='/mods' element={<Mods />} />
        </Routes>
      </MemoryRouter>
    );

    act(() => {
      useAddModStore.setState({
        step: 3,
        url: 'https://example.com/mod/test',
        loader: 'fabric',
        mcVersion: '1.20.1',
        modVersions: [
          {
            id: '1',
            version_type: 'release',
            version_number: '1.0.0',
            date_published: new Date().toISOString(),
          },
        ],
        selectedModVersion: '1',
        versionCache: {
          'example.com:test': {
            versions: ['1.20.1'],
            allModVersions: [],
          },
        },
        modVersionCache: {
          'example.com:test:fabric:1.20.1': [
            {
              id: '1',
              version_type: 'release',
              version_number: '1.0.0',
              date_published: new Date().toISOString(),
            },
          ],
        },
      });
    });

    fireEvent.click(screen.getByRole('button', { name: 'Add' }));

    fireEvent.click(await screen.findByText('Add Another'));

    const urlInput = screen.queryByLabelText('Mod URL');
    const state = useAddModStore.getState();
    expect(state.step).toBe(0);
    expect(state.url).toBe('');
    expect(state.loader).toBe('');
    expect(state.mcVersion).toBe('');
    expect(state.selectedModVersion).toBeNull();
    expect(state.modVersions).toEqual([]);
    expect(state.versions).toEqual([]);
    expect(state.versionCache).toEqual({});
    expect(state.modVersionCache).toEqual({});
    expect(state.includePre).toBe(false);
    expect(state.loadingModVersions).toBe(false);
    expect(state.loadingVersions).toBe(false);
    expect(state.urlError).toBe('');
    expect(state.nonce).toBe(2);
    expect(urlInput).toBeInTheDocument();
    expect(urlInput).toHaveValue('');
    expect(screen.queryByText('1.0.0')).not.toBeInTheDocument();
  });
});
