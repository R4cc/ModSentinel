import { useState } from 'react';
import Header from './Header.jsx';
import Sidebar from './Sidebar.jsx';
import { Toaster } from 'sonner';

export default function Layout({ children }) {
  const [sidebarOpen, setSidebarOpen] = useState(false);

  return (
    <div className="flex min-h-screen bg-background text-foreground">
      <Sidebar open={sidebarOpen} onClose={() => setSidebarOpen(false)} />
      <div className="flex flex-1 flex-col">
        <Header onMenuClick={() => setSidebarOpen(true)} />
        <main className="flex-1 p-md">{children}</main>
      </div>
      <Toaster />
    </div>
  );
}
