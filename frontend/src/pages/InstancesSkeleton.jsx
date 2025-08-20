import { Skeleton } from '@/components/ui/Skeleton.jsx';

export default function InstancesSkeleton() {
  return (
    <div className="space-y-md">
      <div className="flex justify-end">
        <Skeleton className="h-8 w-32" />
      </div>
      <div className="space-y-sm">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} className="h-6 w-full" />
        ))}
      </div>
    </div>
  );
}
