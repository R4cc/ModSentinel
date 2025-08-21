import { Skeleton } from '@/components/ui/Skeleton.jsx';

export default function ModsSkeleton() {
  return (
    <div className="space-y-md">
      <Skeleton className="h-4 w-32" />
      <Skeleton className="h-8 w-64" />
      <div className="space-y-sm">
        {Array.from({ length: 5 }).map((_, i) => (
          <Skeleton key={i} className="h-6 w-full" />
        ))}
      </div>
    </div>
  );
}
