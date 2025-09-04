import { useEffect, useRef, useState } from 'react';
import { Button } from '@/components/ui/Button.jsx';
import { cn } from '@/lib/utils.js';

export default function LoaderSelect({ loaders, value, onChange, disabled, placeholder = 'Select loader' }) {
  const [open, setOpen] = useState(false);
  const ref = useRef(null);
  const sel = loaders.find((l) => l.id === value);

  useEffect(() => {
    function onDoc(e) {
      if (!ref.current) return;
      if (!ref.current.contains(e.target)) setOpen(false);
    }
    if (open) {
      document.addEventListener('mousedown', onDoc);
      return () => document.removeEventListener('mousedown', onDoc);
    }
  }, [open]);

  return (
    <div className="relative" ref={ref}>
      <Button type="button" variant="outline" className="w-full justify-between" onClick={() => !disabled && setOpen((v) => !v)} disabled={disabled} aria-haspopup="listbox" aria-expanded={open}>
        <span className="flex items-center gap-xs truncate">
          {sel?.icon ? (
            <span className="h-4 w-4" aria-hidden dangerouslySetInnerHTML={{ __html: sel.icon }} />
          ) : null}
          <span className={cn('truncate', !sel && 'text-muted-foreground')}>{sel ? (sel.name || sel.id) : placeholder}</span>
        </span>
        <span className="text-muted-foreground">▾</span>
      </Button>
      {open && (
        <div className="absolute z-10 mt-1 w-full rounded-md border bg-background p-xs shadow-md max-h-60 overflow-auto" role="listbox">
          {loaders.map((l) => (
            <button
              key={l.id}
              type="button"
              role="option"
              aria-selected={l.id === value}
              onClick={() => { onChange?.(l.id); setOpen(false); }}
              className={cn('flex w-full items-center gap-sm rounded px-sm py-xs text-left hover:bg-muted', l.id === value && 'bg-muted')}
            >
              {l.icon ? <span className="h-4 w-4" aria-hidden dangerouslySetInnerHTML={{ __html: l.icon }} /> : null}
              <span className="truncate">{l.name || l.id}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

