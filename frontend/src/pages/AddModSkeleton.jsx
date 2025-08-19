import { Skeleton } from '@/components/ui/Skeleton.jsx';

export default function AddModSkeleton() {
  return (
    <div className="space-y-md">
      <Skeleton className="h-4 w-full" />
      <Skeleton className="h-72 w-full" />
    </div>
  );
}
