import { render, screen, fireEvent, act, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import { MemoryRouter, Routes, Route, createMemoryRouter, RouterProvider } from 'react-router-dom';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { axe } from 'vitest-axe';

HTMLCanvasElement.prototype.getContext = () => {};

vi.mock('@/lib/api.ts', () => ({
  getToken: vi.fn().mockResolvedValue('token'),
  addMod: vi.fn(),
  getInstance: vi.fn(),
  getMods: vi.fn(),
}));

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
}));

vi.mock('focus-trap-react', () => ({
  default: ({ children }) => children,
}));

const confirmMock = vi.fn();
vi.mock('@/hooks/useConfirm.jsx', () => ({
  useConfirm: () => ({ confirm: confirmMock, ConfirmModal: null }),
}));

import AddMod from './AddMod.jsx';
import { useAddModStore, initialState } from '@/stores/addModStore.js';
import { useOpenAddMod } from '@/hooks/useOpenAddMod.js';
import { addMod, getInstance, getMods } from '@/lib/api.ts';
import Mods from './Mods.jsx';
import { toast } from 'sonner';

describe('AddMod page', () => {
  beforeEach(() => {
    useAddModStore.setState({
      ...initialState(),
      nonce: 0,
      fetchVersions: vi.fn(),
      fetchModVersions: vi.fn(),
    });
    addMod.mockReset();
    addMod.mockResolvedValue({ mods: [] });
    getInstance.mockResolvedValue({
      id: 1,
      name: 'Inst',
      loader: 'fabric',
      enforce_same_loader: true,
      created_at: '',
      mod_count: 0,
    });
    getMods.mockResolvedValue([]);
    confirmMock.mockResolvedValue(true);
    toast.success.mockReset();
    toast.error.mockReset();
    toast.warning.mockReset();
  });

  it('resets wizard state when reopened after adding a mod', async () => {
    const Mods = () => {
      const openAddMod = useOpenAddMod(1);
      return <button onClick={openAddMod}>Add Another</button>;
    };

    render(
      <MemoryRouter initialEntries={['/instances/1/add']}>
        <Routes>
          <Route path='/instances/:id/add' element={<AddMod />} />
          <Route path='/instances/:id' element={<Mods />} />
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

    const btns = screen.getAllByRole('button', { name: 'Add' });
    fireEvent.click(btns[btns.length - 1]);

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

  it('prefills slug from unresolved file', async () => {
    render(
      <MemoryRouter
        initialEntries={[{ pathname: '/instances/1/add', state: { file: 'sodium-1.0.jar' } }]}
      >
        <Routes>
          <Route path='/instances/:id/add' element={<AddMod />} />
        </Routes>
      </MemoryRouter>,
    );
    const input = await screen.findByLabelText('Mod URL');
    expect(input).toHaveValue('https://modrinth.com/mod/sodium');
  });

  it('shows error toast on loader mismatch when enforced', async () => {
    addMod.mockRejectedValueOnce(new Error('loader mismatch'));

    render(
      <MemoryRouter initialEntries={['/instances/1/add']}>
        <Routes>
          <Route path='/instances/:id/add' element={<AddMod />} />
        </Routes>
      </MemoryRouter>,
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
      });
    });

    await act(async () => {
      const btns = screen.getAllByRole('button', { name: 'Add' });
      fireEvent.click(btns[btns.length - 1]);
    });

    expect(toast.error).toHaveBeenCalledWith('loader mismatch');
  });

  it('warns when loader mismatches but enforcement disabled', async () => {
    addMod.mockResolvedValueOnce({ mods: [], warning: 'loader mismatch' });
    getInstance.mockResolvedValueOnce({
      id: 1,
      name: 'Inst',
      loader: 'fabric',
      enforce_same_loader: false,
      created_at: '',
      mod_count: 0,
    });

    render(
      <MemoryRouter initialEntries={['/instances/1/add']}>
        <Routes>
          <Route path='/instances/:id/add' element={<AddMod />} />
        </Routes>
      </MemoryRouter>,
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
      });
    });

    await act(async () => {
      const btns = screen.getAllByRole('button', { name: 'Add' });
      fireEvent.click(btns[btns.length - 1]);
    });

    expect(toast.warning).toHaveBeenCalledWith('loader mismatch');
    expect(toast.success).toHaveBeenCalledWith('Mod added');
  });

  it('skips loader step when enforcement enabled', async () => {
    render(
      <MemoryRouter initialEntries={['/instances/1/add']}>
        <Routes>
          <Route path='/instances/:id/add' element={<AddMod />} />
        </Routes>
      </MemoryRouter>,
    );

    const urlInput = await screen.findByLabelText('Mod URL');
    fireEvent.change(urlInput, { target: { value: 'https://modrinth.com/mod/test' } });
    const nextBtns = screen.getAllByRole('button', { name: 'Next' });
    fireEvent.click(nextBtns[nextBtns.length - 1]);

    expect(await screen.findByLabelText('Minecraft version')).toBeInTheDocument();
    expect(useAddModStore.getState().loader).toBe('fabric');
  });

  it('preselects instance loader when enforcement disabled', async () => {
    getInstance.mockResolvedValueOnce({
      id: 1,
      name: 'Inst',
      loader: 'quilt',
      enforce_same_loader: false,
      created_at: '',
      mod_count: 0,
    });

    render(
      <MemoryRouter initialEntries={['/instances/1/add']}>
        <Routes>
          <Route path='/instances/:id/add' element={<AddMod />} />
        </Routes>
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(useAddModStore.getState().loader).toBe('quilt');
    });
  });

  it('refreshes mod list after adding a mod', async () => {
    const newMod = {
      id: 1,
      name: 'Alpha',
      url: 'https://example.com/a',
      game_version: '1.20',
      loader: 'fabric',
      current_version: '1.0',
      available_version: '1.0',
      channel: 'release',
      instance_id: 1,
    };
    addMod.mockResolvedValueOnce({ mods: [newMod] });

    render(
      <MemoryRouter initialEntries={['/instances/1/add']}>
        <Routes>
          <Route path='/instances/:id/add' element={<AddMod />} />
          <Route path='/instances/:id' element={<Mods />} />
        </Routes>
      </MemoryRouter>,
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
      });
    });

    const btns = screen.getAllByRole('button', { name: 'Add' });
    fireEvent.click(btns[btns.length - 1]);

    expect(await screen.findByText('Alpha')).toBeInTheDocument();
    expect(getMods).not.toHaveBeenCalled();
  });

  it('navigates back to instances', async () => {
    const router = createMemoryRouter(
      [
        { path: '/instances/:id/add', element: <AddMod /> },
        { path: '/instances', element: <div>Instances</div> },
      ],
      { initialEntries: ['/instances/1/add'] },
    );

    render(<RouterProvider router={router} />);

    const links = await screen.findAllByRole('link', {
      name: /Back to Instances/,
    });
    fireEvent.click(links[links.length - 1]);
    await waitFor(() =>
      expect(router.state.location.pathname).toBe('/instances'),
    );
  });

  it('has no critical axe violations', async () => {
    const { container } = render(
      <MemoryRouter initialEntries={['/instances/1/add']}>
        <Routes>
          <Route path='/instances/:id/add' element={<AddMod />} />
        </Routes>
      </MemoryRouter>
    );
    await screen.findByLabelText('Mod URL');
    const results = await axe(container);
    const critical = results.violations.filter(v => v.impact === 'critical');
    expect(critical).toHaveLength(0);
  });
});
