import { Home, Settings } from 'lucide-react';
import { NavLink } from 'react-router-dom';
import { cn } from '@/lib/utils.js';

export default function Sidebar({ open, onClose }) {
  const linkClass = ({ isActive }) =>
    cn(
      'flex items-center gap-sm rounded-md p-sm text-sm font-medium hover:bg-muted/50',
      isActive && 'bg-muted'
    );

  return (
    <>
      <aside
        className={cn(
          'fixed inset-y-0 left-0 z-20 w-64 transform bg-muted p-md transition-transform md:static md:translate-x-0',
          open ? 'translate-x-0' : '-translate-x-full'
        )}
      >
        <nav className="flex flex-col gap-xs">
          <NavLink to="/" className={linkClass} onClick={onClose}>
            <Home className="h-4 w-4" />
            Dashboard
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
