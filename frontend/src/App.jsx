import { Suspense, lazy } from 'react';
import { Routes, Route, Navigate } from 'react-router-dom';
import Layout from '@/components/Layout.jsx';
import DashboardSkeleton from '@/pages/DashboardSkeleton.jsx';
import InstanceModsSkeleton from '@/pages/InstanceModsSkeleton.jsx';
import AddModSkeleton from '@/pages/AddModSkeleton.jsx';
import SettingsSkeleton from '@/pages/SettingsSkeleton.jsx';
import InstancesListSkeleton from '@/pages/InstancesListSkeleton.jsx';

const Dashboard = lazy(() => import('@/pages/Dashboard.jsx'));
const InstanceMods = lazy(() => import('@/pages/InstanceMods.jsx'));
const AddMod = lazy(() => import('@/pages/AddMod.jsx'));
const Settings = lazy(() => import('@/pages/Settings.jsx'));
const InstancesList = lazy(() => import('@/pages/InstancesList.jsx'));

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
            <Suspense fallback={<InstancesListSkeleton />}>
              <InstancesList />
            </Suspense>
          }
        />
        <Route
          path="/instances/:id"
          element={
            <Suspense fallback={<InstanceModsSkeleton />}>
              <InstanceMods />
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
