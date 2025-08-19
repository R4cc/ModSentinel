import React from 'react';
import { cn } from '@/lib/utils';

function EmptyState({ className, icon: Icon, title, message, ...props }) {
  return (
    <div
      className={cn('flex flex-col items-center justify-center space-y-sm p-xl text-center', className)}
      {...props}
    >
      {Icon && <Icon className='h-8 w-8 text-muted-foreground' aria-hidden='true' />}
      {title && <h2 className='text-lg font-semibold'>{title}</h2>}
      {message && <p className='text-sm text-muted-foreground'>{message}</p>}
    </div>
  );
}

export { EmptyState };
