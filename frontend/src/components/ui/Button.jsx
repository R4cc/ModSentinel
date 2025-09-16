import React from 'react';
import { cn } from '@/lib/utils';

const Button = React.forwardRef(
  (
    {
      className,
      variant = 'default',
      size = 'default',
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
    const sizes = {
      default: 'h-10 px-md py-sm',
      sm: 'h-9 px-sm',
      lg: 'h-11 px-lg',
      icon: 'h-9 w-9 p-0 shrink-0',
    };
    const Comp = as;
    const variantClasses = variants[variant] || variants.default;
    const sizeClasses = sizes[size] || sizes.default;
    return (
      <Comp
        ref={ref}
        type={as === 'button' ? type || 'button' : undefined}
        disabled={as === 'button' ? disabled : undefined}
        aria-disabled={as !== 'button' && disabled ? true : undefined}
        className={cn(
          'inline-flex items-center justify-center rounded-md text-sm font-medium shadow-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 disabled:opacity-50 disabled:pointer-events-none',
          variantClasses,
          sizeClasses,
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
