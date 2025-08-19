import { useEffect, useState } from 'react';
import { Home, Settings, Plus, Package, AlertTriangle } from 'lucide-react';
import { NavLink } from 'react-router-dom';
import { cn } from '@/lib/utils.js';
import { getToken } from '@/lib/api.ts';

export default function Sidebar({ open, onClose }) {
  const linkClass = ({ isActive }) =>
    cn(
      'flex items-center gap-sm rounded-md p-sm text-sm font-medium hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2',
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
          'fixed inset-y-0 left-0 z-20 w-64 transform bg-muted p-md transition-transform md:static md:translate-x-0',
          open ? 'translate-x-0' : '-translate-x-full'
        )}
      >
        <nav className="flex flex-col gap-xs">
          {!hasToken && (
            <NavLink
              to="/settings"
              onClick={onClose}
              className="mb-xs inline-flex w-fit items-center gap-xs rounded-full bg-amber-500 px-sm py-xs text-xs font-semibold text-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2"
            >
              <AlertTriangle className="h-3 w-3" />
              Add token
            </NavLink>
          )}
          <NavLink to="/" end className={linkClass} onClick={onClose}>
            <Home className="h-4 w-4" />
            Dashboard
          </NavLink>
          <NavLink to="/mods" end className={linkClass} onClick={onClose}>
            <Package className="h-4 w-4" />
            Mods
          </NavLink>
          <NavLink
            to="/mods/add"
            className={(s) => cn(linkClass(s), !hasToken && 'pointer-events-none opacity-50')}
            onClick={onClose}
          >
            <Plus className="h-4 w-4" />
            Add Mod
          </NavLink>
          <NavLink to="/settings" className={linkClass} onClick={onClose}>
            <Settings className="h-4 w-4" />
            Settings
          </NavLink>
        </nav>
      </aside>
      {open && <div className="fixed inset-0 z-10 bg-black/50 md:hidden" onClick={onClose} />}
    </>
  );
}
