import { Menu, Shield } from 'lucide-react';

export default function Header({ onMenuClick }) {
  return (
    <header className="flex items-center gap-sm border-b border-border p-md">
      <button
        className="md:hidden rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2"
        onClick={onMenuClick}
        aria-label="Toggle menu"
      >
        <Menu className="h-6 w-6" />
      </button>
      <div className="flex items-center gap-sm">
        <Shield className="h-6 w-6" />
        <h1 className="text-xl font-bold">ModSentinel</h1>
      </div>
    </header>
  );
}
