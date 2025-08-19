import { memo } from 'react';
import { Badge } from '@/components/ui/Badge.jsx';
import { Skeleton } from '@/components/ui/Skeleton.jsx';

function latencyColor(ms) {
  if (ms < 200) return 'bg-green-100 text-green-800';
  if (ms < 500) return 'bg-yellow-100 text-yellow-800';
  return 'bg-red-100 text-red-800';
}

function HealthCard({ data, loading, error }) {
  if (loading) {
    return (
      <div className='space-y-2'>
        {Array.from({ length: 4 }).map((_, i) => (
          <div key={i} className='flex items-center justify-between'>
            <Skeleton className='h-4 w-24' />
            <Skeleton className='h-6 w-20' />
          </div>
        ))}
      </div>
    );
  }

  return (
    <div className='space-y-2'>
      <div className='flex items-center justify-between'>
        <span>Backend</span>
        <Badge
          className={error ? 'bg-red-100 text-red-800' : 'bg-green-100 text-green-800'}
          title={error ? 'Backend offline' : 'Backend online'}
          aria-label={error ? 'Backend offline' : 'Backend online'}
        >
          {error ? 'Offline' : 'Online'}
        </Badge>
      </div>
      <div className='flex items-center justify-between'>
        <span>Latency p50</span>
        <Badge
          className={latencyColor(data?.latency_p50 ?? 0)}
          title={data ? `${data.latency_p50} milliseconds` : 'Not available'}
          aria-label={data ? `Latency p50: ${data.latency_p50} milliseconds` : 'Latency p50 not available'}
        >
          {data ? `${data.latency_p50}ms` : 'N/A'}
        </Badge>
      </div>
      <div className='flex items-center justify-between'>
          <span>Latency p95</span>
          <Badge
            className={latencyColor(data?.latency_p95 ?? 0)}
            title={data ? `${data.latency_p95} milliseconds` : 'Not available'}
            aria-label={data ? `Latency p95: ${data.latency_p95} milliseconds` : 'Latency p95 not available'}
          >
            {data ? `${data.latency_p95}ms` : 'N/A'}
          </Badge>
      </div>
      <div className='flex items-center justify-between'>
        <span>Last sync</span>
        <span
          className='text-sm text-muted-foreground'
          title={data && data.last_sync ? new Date(data.last_sync * 1000).toLocaleString() : 'Never'}
        >
          {data && data.last_sync
            ? new Date(data.last_sync * 1000).toLocaleString()
            : 'Never'}
        </span>
      </div>
    </div>
  );
}

export default memo(HealthCard);
