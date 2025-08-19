import React from 'react';
import { cn } from '@/lib/utils';

function Badge({ className, variant = 'default', ...props }) {
  const variants = {
    default: 'bg-primary text-primary-foreground hover:bg-primary/80',
    secondary: 'bg-muted text-foreground',
  };
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full border border-transparent px-sm py-xs text-xs font-semibold',
        variants[variant],
        className
      )}
      {...props}
    />
  );
}

export { Badge };
