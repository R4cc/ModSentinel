import { useEffect, useState } from 'react';
import { Home, Settings, Package, AlertTriangle } from 'lucide-react';
import { NavLink } from 'react-router-dom';
import { cn } from '@/lib/utils.js';
import { getToken } from '@/lib/api.ts';

export default function Sidebar({ open, onClose }) {
  const linkClass = ({ isActive }) =>
    cn(
      'flex flex-wrap items-center gap-sm md:gap-0 lg:gap-sm rounded-md p-sm text-sm font-medium hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 transition-transform motion-reduce:transition-none motion-safe:hover:translate-x-0.5 motion-safe:focus-visible:translate-x-0.5 justify-start md:justify-center lg:justify-start',
      isActive && 'bg-muted'
    );
  const [hasToken, setHasToken] = useState(true);
  useEffect(() => {
    function update() {
      getToken()
        .then((t) => setHasToken(!!t))
        .catch(() => setHasToken(false));
    }
    update();
    window.addEventListener('token-change', update);
    return () => window.removeEventListener('token-change', update);
  }, []);

  return (
    <>
      <aside
        className={cn(
          'fixed inset-y-0 left-0 z-20 w-64 md:w-16 lg:w-64 transform bg-muted p-md transition-transform md:static md:translate-x-0',
          open ? 'translate-x-0' : '-translate-x-full'
        )}
      >
        <NavLink
          to="/"
          end
          onClick={onClose}
          className="mb-md flex items-center gap-sm md:gap-0 lg:gap-sm rounded-md p-sm justify-start md:justify-center lg:justify-start focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2"
          aria-label="ModSentinel"
          title="ModSentinel"
        >
          <img src="/favicon.ico" alt="ModSentinel" className="h-6 w-6" />
          <span className="hidden text-lg font-bold lg:inline">ModSentinel</span>
        </NavLink>
        <nav className="flex flex-col gap-xs">
          <NavLink
            to="/"
            end
            className={linkClass}
            onClick={onClose}
            aria-label="Dashboard"
            title="Dashboard"
          >
            <Home className="h-4 w-4" aria-hidden="true" />
            <span className="md:hidden lg:inline">Dashboard</span>
          </NavLink>
          <NavLink
            to="/mods"
            end
            className={linkClass}
            onClick={onClose}
            aria-label="Mods"
            title="Mods"
          >
            <Package className="h-4 w-4" aria-hidden="true" />
            <span className="md:hidden lg:inline">Mods</span>
          </NavLink>
          <NavLink
            to="/settings"
            className={linkClass}
            onClick={onClose}
            aria-label="Settings"
            title="Settings"
          >
            <Settings className="h-4 w-4" aria-hidden="true" />
            <span className="md:hidden lg:inline">Settings</span>
            {!hasToken && (
              <span className="ml-auto inline-flex shrink-0 self-stretch items-center gap-xs rounded-md bg-amber-500 px-sm text-xs font-semibold text-white md:hidden lg:inline-flex">
                <AlertTriangle className="h-3 w-3" aria-hidden="true" />
                Add token
              </span>
            )}
          </NavLink>
        </nav>
      </aside>
      {open && (
        <div
          className="fixed inset-0 z-10 bg-black/50 md:hidden"
          onClick={onClose}
          role="button"
          tabIndex={0}
          aria-label="Close menu"
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') onClose();
          }}
        />
      )}
    </>
  );
}
