import { Card, CardHeader, CardContent } from '@/components/ui/Card.jsx';
import { Skeleton } from '@/components/ui/Skeleton.jsx';

export default function DashboardSkeleton() {
  const placeholders = [
    { className: '', height: 'min-h-24' },
    { className: '', height: 'min-h-60' },
    { className: '', height: 'min-h-60' },
    { className: '', height: 'min-h-16' },
    { className: '', height: 'min-h-32' },
    { className: 'sticky bottom-0 z-10 md:static', height: 'min-h-24' },
  ];

  return (
    <div className='grid grid-cols-1 gap-md lg:grid-cols-2'>
      {placeholders.map(({ className, height }, idx) => (
        <Card key={idx} className={className}>
          <CardHeader>
            <Skeleton className='h-6 w-24' />
          </CardHeader>
          <CardContent className={height}>
            <Skeleton className='h-full w-full' />
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
