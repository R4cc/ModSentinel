import { memo } from 'react';
import { Link } from 'react-router-dom';
import { Skeleton } from '@/components/ui/Skeleton.jsx';

function SummaryCard({ data, loading, error }) {
  if (loading) {
    return (
      <div className='grid grid-cols-3 gap-md text-center'>
        {Array.from({ length: 3 }).map((_, i) => (
          <div key={i} className='flex flex-col items-center space-y-1'>
            <Skeleton className='h-8 w-12' />
            <Skeleton className='h-4 w-16' />
          </div>
        ))}
      </div>
    );
  }

  if (error) {
    return <p className='text-destructive'>{error}</p>;
  }

  if (!data || data.tracked === 0) {
    return <p className='text-muted-foreground'>No mods tracked.</p>;
  }

  const items = [
    { label: 'Instances', value: data.instances ?? 0, to: '/instances' },
    { label: 'Tracked mods', value: data.tracked, to: '/instances' },
    { label: 'Outdated', value: data.outdated, to: '/instances' },
  ];

  return (
    <div className='grid grid-cols-3 gap-md text-center'>
      {items.map((item) => (
        <Link
          key={item.label}
          to={item.to}
          className='flex flex-col items-center focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 rounded'
          title={`${item.label}: ${item.value}`}
          aria-label={`${item.label}: ${item.value}`}
        >
          <span className='text-2xl font-bold'>{item.value}</span>
          <span className='text-sm text-muted-foreground'>{item.label}</span>
        </Link>
      ))}
    </div>
  );
}

export default memo(SummaryCard);
