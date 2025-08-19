import { useEffect, lazy, Suspense } from 'react';
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/Card.jsx';
import { Skeleton } from '@/components/ui/Skeleton.jsx';
import SummaryCard from '@/components/dashboard/SummaryCard.jsx';
import OutdatedCard from '@/components/dashboard/OutdatedCard.jsx';
const UpdatesCard = lazy(() => import('@/components/dashboard/UpdatesCard.jsx'));
import AlertsCard from '@/components/dashboard/AlertsCard.jsx';
import QuickActionsCard from '@/components/dashboard/QuickActionsCard.jsx';
const HealthCard = lazy(() => import('@/components/dashboard/HealthCard.jsx'));
import { useDashboardStore } from '@/stores/dashboardStore.js';
import { emitDashboardRefresh, onDashboardRefresh } from '@/lib/refresh.js';

const sections = [
  { id: 'summary', title: 'Summary', height: 'min-h-24' },
  { id: 'outdated', title: 'Outdated', height: 'min-h-60' },
  { id: 'updates', title: 'Updates', height: 'min-h-60' },
  { id: 'alerts', title: 'Alerts', height: 'min-h-16' },
  { id: 'health', title: 'Health', height: 'min-h-32' },
  {
    id: 'quick-actions',
    title: 'Quick actions',
    className: 'sticky bottom-0 z-10 md:static',
    height: 'min-h-24',
  },
];

export default function Dashboard() {
  const { data, loading, error, fetch } = useDashboardStore();

  useEffect(() => {
    const handler = (opts) => fetch(opts);
    const unsub = onDashboardRefresh(handler);
    emitDashboardRefresh();
    const id = setInterval(() => emitDashboardRefresh(), 60000);
    return () => {
      unsub();
      clearInterval(id);
    };
  }, [fetch]);

  return (
    <div className='grid grid-cols-1 gap-md lg:grid-cols-2'>
      {sections.map(({ id, title, className, height }) => (
        <Card
          key={id}
          tabIndex={0}
          aria-labelledby={`${id}-title`}
          className={`${className ?? ''} focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2`}
        >
          <CardHeader>
            <CardTitle id={`${id}-title`}>{title}</CardTitle>
          </CardHeader>
          <CardContent className={height}>
            {id === 'summary' ? (
              <SummaryCard data={data} loading={loading} error={error} />
            ) : id === 'outdated' ? (
              <OutdatedCard data={data} loading={loading} error={error} />
            ) : id === 'updates' ? (
              <Suspense fallback={<Skeleton className='h-full w-full' />}>
                <UpdatesCard data={data} loading={loading} error={error} />
              </Suspense>
            ) : id === 'alerts' ? (
              <AlertsCard error={error} onRetry={() => emitDashboardRefresh({ force: true })} />
            ) : id === 'quick-actions' ? (
              <QuickActionsCard />
            ) : id === 'health' ? (
              <Suspense fallback={<Skeleton className='h-full w-full' />}>
                <HealthCard data={data} loading={loading} error={error} />
              </Suspense>
            ) : error ? (
              <p className='text-destructive'>{error}</p>
            ) : loading ? (
              <Skeleton className='h-full w-full' />
            ) : (
              <p className='text-muted-foreground'>No data available.</p>
            )}
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
