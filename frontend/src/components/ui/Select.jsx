import React from 'react';
import { cn } from '@/lib/utils';

const Select = React.forwardRef(({ className, ...props }, ref) => {
  return (
    <select
      ref={ref}
      className={cn(
        'flex h-10 w-full rounded-md border border-border bg-transparent px-md py-sm text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50',
        className
      )}
      {...props}
    />
  );
});
Select.displayName = 'Select';

export { Select };
