import { Routes, Route } from 'react-router-dom';
import Layout from '@/components/Layout.jsx';
import Dashboard from '@/pages/Dashboard.jsx';
import Settings from '@/pages/Settings.jsx';

export default function App() {
  return (
    <Layout>
      <Routes>
        <Route path="/" element={<Dashboard />} />
        <Route path="/settings" element={<Settings />} />
      </Routes>
    </Layout>
  );
}
