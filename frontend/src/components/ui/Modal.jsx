import React, { useEffect } from 'react';
import FocusTrap from 'focus-trap-react';
import { cn } from '@/lib/utils';

function Modal({ open, onClose, className, children, ...props }) {
  useEffect(() => {
    function handleKey(e) {
      if (e.key === 'Escape') {
        onClose();
      }
    }
    if (open) {
      document.addEventListener('keydown', handleKey);
    }
    return () => document.removeEventListener('keydown', handleKey);
  }, [open, onClose]);

  if (!open) return null;
  return (
    <div className='fixed inset-0 z-50 flex items-center justify-center'>
      <div
        className='absolute inset-0 bg-black/50'
        onClick={onClose}
        aria-hidden='true'
      />
      <FocusTrap focusTrapOptions={{ returnFocusOnDeactivate: true }}>
        <div
          role='dialog'
          aria-modal='true'
          className={cn('relative z-10 w-full max-w-lg rounded-md bg-background p-lg shadow-md', className)}
          {...props}
        >
          {children}
        </div>
      </FocusTrap>
    </div>
  );
}

export { Modal };
