import { useState, useCallback, memo } from 'react';
import { Button } from '@/components/ui/Button.jsx';
import { Skeleton } from '@/components/ui/Skeleton.jsx';
import { useDashboardStore } from '@/stores/dashboardStore.js';
import { toast } from 'sonner';

function OutdatedCard({ data, loading, error }) {
  const update = useDashboardStore((s) => s.update);
  const [failed, setFailed] = useState({});

  const handleUpdate = useCallback(
    async (m) => {
      try {
        await update(m);
        toast.success(`Updated ${m.name}`);
        setFailed((f) => ({ ...f, [m.id]: false }));
      } catch {
        toast.error(`Failed to update ${m.name}`);
        setFailed((f) => ({ ...f, [m.id]: true }));
      }
    },
    [update]
  );

  if (loading) {
    return (
      <ul className='space-y-2'>
        {Array.from({ length: 5 }).map((_, i) => (
          <li key={i} className='flex items-center justify-between'>
            <div>
              <Skeleton className='h-4 w-32' />
              <Skeleton className='mt-1 h-3 w-24' />
            </div>
            <Skeleton className='h-8 w-16' />
          </li>
        ))}
      </ul>
    );
  }

  if (error) {
    return <p className='text-destructive'>{error}</p>;
  }

  if (!data || data.outdated_mods.length === 0) {
    return <p className='text-muted-foreground'>All mods up to date.</p>;
  }

  return (
    <ul className='space-y-2'>
      {data.outdated_mods.map((m) => (
        <OutdatedItem
          key={m.id}
          mod={m}
          onUpdate={handleUpdate}
          failed={failed[m.id]}
        />
      ))}
    </ul>
  );
}

const OutdatedItem = memo(function OutdatedItem({ mod, onUpdate, failed }) {
  return (
    <li className='flex items-center justify-between gap-sm'>
      <div className='flex flex-col'>
        <span className='font-medium'>{mod.name || mod.url}</span>
        <span className='text-sm text-muted-foreground'>
          {mod.current_version} → {mod.available_version}
        </span>
      </div>
      {failed ? (
        <Button size='sm' variant='outline' onClick={() => onUpdate(mod)}>
          Retry
        </Button>
      ) : (
        <Button size='sm' onClick={() => onUpdate(mod)}>
          Update
        </Button>
      )}
    </li>
  );
});

export default memo(OutdatedCard);
