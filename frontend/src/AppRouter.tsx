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
    localStorage.removeItem('onboarding_done');
    localStorage.removeItem('walkthrough_done');
    nav('/chat', { replace: true });
  }, [nav]);
  return null;
}

export default function AppRouter() {
  return (
    <HashRouter>
      <Routes>
        <Route element={<AppLayout />}>
          <Route index element={<HomePage />} />
          <Route path="chat" element={<ChatView />} />
          <Route path="chat/:threadId" element={<ChatView />} />
          <Route path="memory" element={<MemorySearch />} />
          <Route path="dashboard" element={<DashboardPage />} />
          <Route path="settings/*" element={<SettingsPage />} />
          <Route path="capabilities" element={<CapabilitiesPage />} />
          <Route path="notifications" element={<NotificationCenter />} />
          <Route path="monitor" element={<MonitorPanel />} />
        </Route>
        <Route path="/reset" element={<ResetOnboarding />} />
        <Route path="*" element={<Navigate to="/chat" replace />} />
      </Routes>
    </HashRouter>
  );
}
