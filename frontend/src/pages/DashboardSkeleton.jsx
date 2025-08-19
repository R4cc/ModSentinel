import { Skeleton } from '@/components/ui/Skeleton.jsx';

export default function DashboardSkeleton() {
  return (
    <div className="space-y-md">
      <Skeleton className="h-8 w-32" />
      <Skeleton className="h-4 w-full" />
      <Skeleton className="h-4 w-full" />
    </div>
  );
}
