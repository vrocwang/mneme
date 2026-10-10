import { useEffect } from 'react';
import { HashRouter, Routes, Route, Navigate, useNavigate, useLocation, Outlet } from 'react-router-dom';
import { Sidebar } from './components/layout/Sidebar';
import { ErrorBoundary } from './components/layout/ErrorBoundary';
import { HomePage } from './components/home/HomePage';
import { ChatView } from './components/chat/ChatView';
import { SettingsPage } from './components/settings/SettingsPage';
import { DashboardPage } from './components/dashboard/DashboardPage';
import { MemorySearch } from './components/memory/MemorySearch';
import { CapabilitiesPage } from './components/capabilities/CapabilitiesPage';
import { NotificationCenter } from './components/notifications/NotificationCenter';
import { NotificationToast } from './components/notifications/NotificationToast';
import { MonitorPanel } from './components/monitor/MonitorPanel';
import { ROUTES, routeSegment } from './lib/routes';
import { StorageKeys, removeValue } from './lib/storage';

function AppLayout() {
  const location = useLocation();
  return (
    <div className="flex h-screen bg-surface overflow-hidden">
      <Sidebar />
      <main className="flex-1 flex flex-col min-w-0">
        <ErrorBoundary key={location.pathname}>
          <Outlet />
        </ErrorBoundary>
      </main>
      <NotificationToast />
    </div>
  );
}

function ResetOnboarding() {
  const nav = useNavigate();
  useEffect(() => {
    removeValue(StorageKeys.onboardingDone);
    removeValue(StorageKeys.walkthroughDone);
    nav(ROUTES.chat, { replace: true });
  }, [nav]);
  return null;
}

export default function AppRouter() {
  return (
    <HashRouter>
      <Routes>
        <Route element={<AppLayout />}>
          <Route index element={<HomePage />} />
          <Route path={routeSegment('chat')} element={<ChatView />} />
          <Route path={`${routeSegment('chat')}/:threadId`} element={<ChatView />} />
          <Route path={routeSegment('memory')} element={<MemorySearch />} />
          <Route path={routeSegment('dashboard')} element={<DashboardPage />} />
          <Route path={`${routeSegment('settings')}/*`} element={<SettingsPage />} />
          <Route path={routeSegment('capabilities')} element={<CapabilitiesPage />} />
          <Route path={routeSegment('notifications')} element={<NotificationCenter />} />
          <Route path={routeSegment('monitor')} element={<MonitorPanel />} />
        </Route>
        <Route path={ROUTES.reset} element={<ResetOnboarding />} />
        <Route path="*" element={<Navigate to={ROUTES.chat} replace />} />
      </Routes>
    </HashRouter>
  );
}
