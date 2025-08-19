import { useEffect, useState } from 'react';
import { useSearchParams, Link } from 'react-router-dom';
import { Package, RefreshCw, Trash2, Plus } from 'lucide-react';
import { Input } from '@/components/ui/Input.jsx';
import { Select } from '@/components/ui/Select.jsx';
import { Button } from '@/components/ui/Button.jsx';
import {
  Table,
  TableHeader,
  TableRow,
  TableHead,
  TableBody,
  TableCell,
} from '@/components/ui/Table.jsx';
import { Skeleton } from '@/components/ui/Skeleton.jsx';
import { EmptyState } from '@/components/ui/EmptyState.jsx';
import { getMods, refreshMod, deleteMod, getToken } from '@/lib/api.ts';
import { cn } from '@/lib/utils.js';
import { toast } from 'sonner';
import { useConfirm } from '@/hooks/useConfirm.jsx';

export default function Mods() {
  const [mods, setMods] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [filter, setFilter] = useState('');
  const [searchParams] = useSearchParams();
  const status = searchParams.get('status') || 'all';
  const [sort, setSort] = useState('name-asc');
  const [page, setPage] = useState(1);
  const perPage = 10;
  const { confirm, ConfirmModal } = useConfirm();
  const [hasToken, setHasToken] = useState(true);

  useEffect(() => {
    getToken()
      .then((t) => setHasToken(!!t))
      .catch(() => setHasToken(false));
  }, []);

  useEffect(() => {
    fetchMods();
  }, []);

  async function fetchMods() {
    setLoading(true);
    setError('');
    try {
      const data = await getMods();
      setMods(data);
    } catch {
      setError('Failed to load mods');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    setPage(1);
  }, [filter, sort, status]);

  const filtered = mods.filter((m) =>
    m.name.toLowerCase().includes(filter.toLowerCase())
  );
  const statusFiltered = filtered.filter((m) => {
    if (status === 'up_to_date') return m.current_version === m.available_version;
    if (status === 'outdated') return m.current_version !== m.available_version;
    return true;
  });
  const sorted = [...statusFiltered].sort((a, b) =>
    sort === 'name-desc'
      ? b.name.localeCompare(a.name)
      : a.name.localeCompare(b.name)
  );
  const totalPages = Math.ceil(sorted.length / perPage) || 1;
  const current = sorted.slice((page - 1) * perPage, page * perPage);

  async function handleCheck(m) {
    try {
      const data = await refreshMod(m.id, {
        url: m.url,
        loader: m.loader,
        game_version: m.game_version,
        channel: m.channel,
      });
      setMods(data);
      const updated = data.find((x) => x.id === m.id);
      if (updated && updated.available_version !== updated.current_version) {
        toast.info(`New version ${updated.available_version} available`);
      } else {
        toast.success('Mod is up to date');
      }
    } catch (err) {
      if (err instanceof Error && err.message === 'token required') {
        toast.error('Modrinth token required');
      } else {
        toast.error('Failed to check updates');
      }
    }
  }

  async function handleDelete(id) {
    const ok = await confirm({
      title: 'Delete mod',
      message: 'Are you sure you want to delete this mod?',
      confirmText: 'Delete',
    });
    if (!ok) return;
    try {
      const data = await deleteMod(id);
      setMods(data);
      toast.success('Mod deleted');
    } catch {
      toast.error('Failed to delete mod');
    }
  }

  return (
    <div className="space-y-md">
      {ConfirmModal}
        <div className="flex flex-col gap-sm sm:flex-row sm:items-center">
          <div className="flex flex-col">
            <label htmlFor="filter" className="sr-only">
              Filter mods
            </label>
            <Input
              id="filter"
              placeholder="Filter mods..."
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              className="sm:max-w-xs"
            />
          </div>
          <div className="flex flex-col">
            <label htmlFor="sort" className="sr-only">
              Sort mods
            </label>
            <Select
              id="sort"
              value={sort}
              onChange={(e) => setSort(e.target.value)}
              className="sm:w-40"
            >
              <option value="name-asc">Name A–Z</option>
              <option value="name-desc">Name Z–A</option>
            </Select>
          </div>
          <Link
            to="/mods/add"
            className={cn(
              'sm:ml-auto focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2',
              !hasToken && 'pointer-events-none opacity-50'
            )}
            aria-disabled={!hasToken}
            title="Add Mod"
          >
            <Button className="w-full sm:w-auto gap-xs" disabled={!hasToken}>
              <Plus className="h-4 w-4" aria-hidden="true" />
              Add Mod
            </Button>
          </Link>
        </div>

      {!hasToken && (
        <p className="text-sm text-muted-foreground">
          Set a Modrinth token in settings to enable update checks.
        </p>
      )}

      {loading && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Game</TableHead>
              <TableHead>Loader</TableHead>
              <TableHead>Current</TableHead>
              <TableHead>Available</TableHead>
              <TableHead>Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {Array.from({ length: 5 }).map((_, i) => (
              <TableRow key={i}>
                <TableCell>
                  <Skeleton className="h-4 w-32" />
                </TableCell>
                <TableCell>
                  <Skeleton className="h-4 w-20" />
                </TableCell>
                <TableCell>
                  <Skeleton className="h-4 w-16" />
                </TableCell>
                <TableCell>
                  <Skeleton className="h-4 w-20" />
                </TableCell>
                <TableCell>
                  <Skeleton className="h-4 w-20" />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      {!loading && error && (
        <div className="flex flex-col items-center gap-sm">
          <p className="text-sm text-muted-foreground">{error}</p>
          <Button onClick={fetchMods}>Retry</Button>
        </div>
      )}

      {!loading && !error && current.length === 0 && (
        <EmptyState
          icon={Package}
          title="No mods"
          message="You haven't added any mods yet."
        />
      )}

      {!loading && !error && current.length > 0 && (
        <>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Game</TableHead>
                <TableHead>Loader</TableHead>
                <TableHead>Current</TableHead>
                <TableHead>Available</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {current.map((m) => (
                <TableRow key={m.id}>
                  <TableCell className="flex items-center gap-sm font-medium">
                    {m.icon_url && (
                      <img
                        src={m.icon_url}
                        alt=""
                        className="h-6 w-6 rounded-sm"
                      />
                    )}
                    {m.name || m.url}
                  </TableCell>
                  <TableCell>{m.game_version}</TableCell>
                  <TableCell>{m.loader}</TableCell>
                  <TableCell>{m.current_version}</TableCell>
                  <TableCell>{m.available_version}</TableCell>
                  <TableCell className="flex gap-xs">
                    <Button
                      variant="outline"
                      onClick={() => handleCheck(m)}
                      aria-label="Check for updates"
                      className="h-8 px-sm"
                      disabled={!hasToken}
                    >
                      <RefreshCw className="h-4 w-4" aria-hidden="true" />
                    </Button>
                    <Button
                      variant="outline"
                      onClick={() => handleDelete(m.id)}
                      aria-label="Delete mod"
                      className="h-8 px-sm"
                    >
                      <Trash2 className="h-4 w-4" aria-hidden="true" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>

          <div className="flex items-center justify-between pt-sm">
            <Button
              variant="outline"
              onClick={() => setPage((p) => p - 1)}
              disabled={page === 1}
            >
              Previous
            </Button>
            <span className="text-sm text-muted-foreground">
              Page {page} of {totalPages}
            </span>
            <Button
              variant="outline"
              onClick={() => setPage((p) => p + 1)}
              disabled={page >= totalPages}
            >
              Next
            </Button>
          </div>
        </>
      )}
    </div>
  );
}

