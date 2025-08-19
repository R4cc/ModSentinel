import { memo, useMemo } from 'react';
import { Skeleton } from '@/components/ui/Skeleton.jsx';

function UpdatesCard({ data, loading, error }) {
  if (loading) {
    return (
      <ul className='space-y-4'>
        {Array.from({ length: 2 }).map((_, i) => (
          <li key={i}>
            <Skeleton className='h-4 w-24' />
            <ul className='mt-1 space-y-1'>
              {Array.from({ length: 3 }).map((__, j) => (
                <li key={j} className='flex justify-between text-sm'>
                  <Skeleton className='h-4 w-32' />
                  <Skeleton className='h-4 w-20' />
                </li>
              ))}
            </ul>
          </li>
        ))}
      </ul>
    );
  }

  if (error) {
    return <p className='text-destructive'>{error}</p>;
  }

  if (!data || data.recent_updates.length === 0) {
    return <p className='text-muted-foreground'>No updates.</p>;
  }

  const groups = useMemo(() => {
    return data.recent_updates.reduce((acc, u) => {
      const day = new Date(u.updated_at).toLocaleDateString();
      (acc[day] ||= []).push(u);
      return acc;
    }, {});
  }, [data]);

  return (
    <ul className='space-y-4'>
      {Object.entries(groups).map(([day, items]) => (
        <UpdateGroup key={day} day={day} items={items} />
      ))}
    </ul>
  );
}

const UpdateGroup = memo(function UpdateGroup({ day, items }) {
  return (
    <li>
      <p className='text-sm font-medium'>{day}</p>
      <ul className='mt-1 space-y-1'>
        {items.map((u) => (
          <li key={u.id} className='flex justify-between text-sm'>
            <span>{u.name}</span>
            <span className='text-muted-foreground'>
              {u.version} · {new Date(u.updated_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
            </span>
          </li>
        ))}
      </ul>
    </li>
  );
});

export default memo(UpdatesCard);
