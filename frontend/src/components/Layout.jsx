import { useState } from 'react';
import usePreferences from '@/hooks/usePreferences.js';
import Header from './Header.jsx';
import Sidebar from './Sidebar.jsx';
import { Toaster } from '@/components/ui/Toaster.jsx';

export default function Layout({ children }) {
  const [sidebarOpen, setSidebarOpen] = useState(false);
  usePreferences();

  return (
    <div className="flex min-h-screen bg-background text-foreground">
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:absolute focus:top-md focus:left-md focus:bg-background focus:p-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2"
      >
        Skip to content
      </a>
      <Sidebar open={sidebarOpen} onClose={() => setSidebarOpen(false)} />
      <div className="flex flex-1 flex-col">
        <Header onMenuClick={() => setSidebarOpen(true)} />
        <main id="main" className="flex-1 p-md">
          {children}
        </main>
      </div>
      <Toaster />
    </div>
  );
}
