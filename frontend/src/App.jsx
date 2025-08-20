import { Suspense, lazy } from 'react';
import { Routes, Route, Navigate } from 'react-router-dom';
import Layout from '@/components/Layout.jsx';
import DashboardSkeleton from '@/pages/DashboardSkeleton.jsx';
import ModsSkeleton from '@/pages/ModsSkeleton.jsx';
import AddModSkeleton from '@/pages/AddModSkeleton.jsx';
import SettingsSkeleton from '@/pages/SettingsSkeleton.jsx';
import InstancesSkeleton from '@/pages/InstancesSkeleton.jsx';

const Dashboard = lazy(() => import('@/pages/Dashboard.jsx'));
const Mods = lazy(() => import('@/pages/Mods.jsx'));
const AddMod = lazy(() => import('@/pages/AddMod.jsx'));
const Settings = lazy(() => import('@/pages/Settings.jsx'));
const Instances = lazy(() => import('@/pages/Instances.jsx'));

export default function App() {
  return (
    <Layout>
      <Routes>
        <Route
          path="/"
          element={
            <Suspense fallback={<DashboardSkeleton />}>
              <Dashboard />
            </Suspense>
          }
        />
        <Route
          path="/instances"
          element={
            <Suspense fallback={<InstancesSkeleton />}>
              <Instances />
            </Suspense>
          }
        />
        <Route
          path="/instances/:id"
          element={
            <Suspense fallback={<ModsSkeleton />}>
              <Mods />
            </Suspense>
          }
        />
        <Route
          path="/instances/:id/add"
          element={
            <Suspense fallback={<AddModSkeleton />}>
              <AddMod />
            </Suspense>
          }
        />
        <Route path="/mods/*" element={<Navigate to="/instances" replace />} />
        <Route
          path="/settings"
          element={
            <Suspense fallback={<SettingsSkeleton />}>
              <Settings />
            </Suspense>
          }
        />
      </Routes>
    </Layout>
  );
}