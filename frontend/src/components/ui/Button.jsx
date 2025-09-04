import React from 'react';
import { cn } from '@/lib/utils';

const Button = React.forwardRef(
  (
    {
      className,
      variant = 'default',
      as = 'button',
      type,
      disabled,
      ...props
    },
    ref,
  ) => {
    const variants = {
      default: 'bg-primary text-primary-foreground hover:bg-primary/90',
      outline: 'border border-border bg-transparent hover:bg-muted',
      // Secondary: subtle filled button matching cards/inputs
      secondary: 'bg-muted text-foreground hover:bg-muted/80 border border-border',
    };
    const Comp = as;
    return (
      <Comp
        ref={ref}
        type={as === 'button' ? type || 'button' : undefined}
        disabled={as === 'button' ? disabled : undefined}
        aria-disabled={as !== 'button' && disabled ? true : undefined}
        className={cn(
          'inline-flex items-center justify-center rounded-md text-sm font-medium shadow-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 disabled:opacity-50 disabled:pointer-events-none h-10 px-md py-sm',
          variants[variant],
          className,
          disabled && as !== 'button' && 'opacity-50 pointer-events-none',
        )}
        {...props}
      />
    );
  },
);
Button.displayName = 'Button';

export { Button };
