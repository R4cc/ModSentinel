import { Skeleton } from '@/components/ui/Skeleton.jsx';

export default function SettingsSkeleton() {
  return (
    <div className="space-y-md">
      <Skeleton className="h-8 w-40" />
      <div className="space-y-sm">
        <Skeleton className="h-10 w-full sm:w-64" />
        <Skeleton className="h-10 w-full sm:w-64" />
      </div>
    </div>
  );
}
