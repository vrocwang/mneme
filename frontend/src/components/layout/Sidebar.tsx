import { useState } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import { useApp } from '../../state/AppContext';
import { useT } from '../../lib/i18n/I18nContext';
import { useThreads } from '../../hooks/useThreads';
import { useApprovals } from '../../hooks/useApprovals';
import { ThemeSwitcher } from '../ThemeSwitcher';
import { ConnectivityIndicator } from '../ConnectivityIndicator';

const NAV_ITEMS = [
  { id: 'home' as const, key: 'nav.home', icon: HomeIcon },
  { id: 'chat' as const, key: 'nav.chat', icon: ChatIcon },
  { id: 'memory' as const, key: 'nav.memory', icon: MemoryIcon },
  { id: 'capabilities' as const, key: 'nav.capabilities', icon: CapabilitiesIcon },
  { id: 'dashboard' as const, key: 'nav.dashboard', icon: DashboardIcon },
  { id: 'notifications' as const, key: 'nav.notifications', icon: NotificationsIcon },
  { id: 'monitor' as const, key: 'nav.monitor', icon: MonitorIcon },
  { id: 'settings' as const, key: 'nav.settings', icon: SettingsIcon },
];

export function Sidebar() {
  const navigate = useNavigate();
  const location = useLocation();
  const { state, actions } = useApp();
  const { t } = useT();
  const [search, setSearch] = useState('');

  const { threads, activeThreadId, selectThread, createNewThread, deleteSelectedThread } = useThreads();
  const { pendingApprovals } = useApprovals();

  const filteredThreads = threads.filter(th =>
    !search || th.title.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <>
    {/* Collapsed toggle — always visible when sidebar is closed */}
    {!state.sidebarOpen && (
      <button
        onClick={actions.toggleSidebar}
        className="fixed left-2 top-3 z-50 p-2 rounded-lg bg-surface-elevated border border-surface-border hover:bg-white/5 transition-colors"
        title={t('sidebar.openSidebar', 'Open sidebar')}
      >
        <svg className="w-4 h-4 text-white/50" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
        </svg>
      </button>
    )}
    <aside className={`${state.sidebarOpen ? 'w-60' : 'w-0'} transition-all duration-300 bg-surface-elevated border-r border-surface-border flex flex-col overflow-hidden`}>
      {/* Logo */}
      <div className="flex items-center gap-2 px-4 py-3 border-b border-surface-border shrink-0">
        <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-ocean-400 to-ocean-600 flex items-center justify-center text-white font-bold text-sm">
          OH
        </div>
        <span className="font-semibold text-white/90 tracking-tight">{t('app.title')}</span>
        <button className="btn-ghost ml-auto !p-1.5" onClick={actions.toggleSidebar} title={t('sidebar.closeSidebar')}>
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 19l-7-7 7-7m8 14l-7-7 7-7" />
          </svg>
        </button>
      </div>

      {/* Navigation */}
      <nav className="flex flex-col gap-0.5 px-2 py-2 border-b border-surface-border shrink-0">
        {NAV_ITEMS.map(item => {
          const isActive = location.pathname === '/' + item.id || (item.id === 'home' && location.pathname === '/');
          return (
            <button
              key={item.id}
              data-nav={item.id}
              onClick={() => navigate(item.id === 'home' ? '/' : `/${item.id}`)}
              className={`flex items-center gap-2 px-2.5 py-1.5 rounded-md text-xs font-medium transition-all duration-200 ${
                isActive
                  ? 'bg-ocean-500/15 text-ocean-300 shadow-inner-glow'
                  : 'text-white/50 hover:text-white/80 hover:bg-white/5'
              }`}
            >
              <item.icon active={isActive} />
              {t(item.key)}
              {item.id === 'notifications' && pendingApprovals.length > 0 && (
                <span className="ml-auto bg-coral-500/20 text-coral-400 text-[10px] font-medium px-1.5 py-0.5 rounded-full leading-none">
                  {pendingApprovals.length}
                </span>
              )}
            </button>
          );
        })}
      </nav>

      {/* Thread section */}
      <div className="flex-1 flex flex-col min-h-0">
        <div className="flex items-center justify-between px-3 py-2 shrink-0">
          <span className="text-xs font-semibold text-white/40 uppercase tracking-wider">{t('sidebar.threads')}</span>
          <button
            onClick={async () => { const t = await createNewThread(); if (t) navigate(`/chat/${t.id}`); }}
            className="p-1 hover:bg-white/10 rounded-md transition-colors"
            title={t('sidebar.newThread')}
          >
            <svg className="w-4 h-4 text-white/50" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
            </svg>
          </button>
        </div>

        {threads.length > 0 && (
          <div className="px-3 pb-2 shrink-0">
            <input
              className="input-field !py-1.5 !text-xs"
              placeholder={t('sidebar.searchThreads')}
              value={search}
              onChange={e => setSearch(e.target.value)}
            />
          </div>
        )}

        <div className="flex-1 overflow-y-auto px-3 space-y-0.5">
          {filteredThreads.map((thread, i) => (
            <div
              key={thread.id || `thread-${i}`}
              className={`group flex items-center gap-2 px-2.5 py-1.5 rounded-md cursor-pointer transition-all duration-200 ${
                activeThreadId === thread.id
                  ? 'bg-ocean-500/10 border border-ocean-500/20'
                  : 'hover:bg-white/5 border border-transparent'
              }`}
              onClick={async () => { await selectThread(thread.id); navigate(`/chat/${thread.id}`); }}
            >
              <div className="flex-1 min-w-0">
                <div className="text-xs text-white/80 truncate" title={thread.title || undefined}>{thread.title || t('chat.newConversation')}</div>
                <div className="text-[10px] text-white/30">{thread.message_count || 0} {t('sidebar.msgs')}</div>
              </div>
              <button
                className="opacity-0 pointer-events-none group-hover:opacity-100 group-hover:pointer-events-auto p-1 hover:bg-coral-500/20 rounded transition-all"
                onClick={e => { e.stopPropagation(); deleteSelectedThread(thread.id); }}
                title={t('sidebar.deleteThread')}
              >
                <svg className="w-3.5 h-3.5 text-coral-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                </svg>
              </button>
            </div>
          ))}
          {filteredThreads.length === 0 && (
            <div className="text-center text-white/25 text-sm py-8">
              {threads.length === 0 ? t('sidebar.noThreads') : t('sidebar.noMatch')}
            </div>
          )}
        </div>
      </div>

      {/* Bottom status */}
      <div className="px-4 py-2 border-t border-surface-border space-y-2 shrink-0">
        <div className="flex items-center gap-2">
          <div className="w-2 h-2 rounded-full bg-sage-400 animate-pulse-soft" />
          <span className="text-xs text-white/30">{t('sidebar.count', { count: threads.length })}</span>
          {pendingApprovals.length > 0 && (
            <span className="badge bg-amber-500/20 text-amber-400 ml-auto text-[10px]">{pendingApprovals.length} {t('approval.pending')}</span>
          )}
        </div>
        <div className="flex items-center justify-between">
          <ConnectivityIndicator />
          <ThemeSwitcher />
        </div>
      </div>
    </aside>
    </>
  );
}

// ── Icons ──────────────────────────────────────────────────────────────────

function ChatIcon({ active }: { active: boolean }) {
  return (
    <svg className={`w-4 h-4 ${active ? 'text-ocean-400' : 'text-white/40'}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.8} d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
    </svg>
  );
}

function MemoryIcon({ active }: { active: boolean }) {
  return (
    <svg className={`w-4 h-4 ${active ? 'text-ocean-400' : 'text-white/40'}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.8} d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4" />
    </svg>
  );
}

function DashboardIcon({ active }: { active: boolean }) {
  return (
    <svg className={`w-4 h-4 ${active ? 'text-ocean-400' : 'text-white/40'}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.8} d="M4 5a1 1 0 011-1h14a1 1 0 011 1v2a1 1 0 01-1 1H5a1 1 0 01-1-1V5zm0 8a1 1 0 011-1h6a1 1 0 011 1v6a1 1 0 01-1 1H5a1 1 0 01-1-1v-6zm12 0a1 1 0 011-1h2a1 1 0 011 1v6a1 1 0 01-1 1h-2a1 1 0 01-1-1v-6z" />
    </svg>
  );
}

function CapabilitiesIcon({ active }: { active: boolean }) {
  return (
    <svg className={`w-4 h-4 ${active ? 'text-ocean-400' : 'text-white/40'}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.8} d="M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zm10 0a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zm10 0a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z" />
    </svg>
  );
}

function SettingsIcon({ active }: { active: boolean }) {
  return (
    <svg className={`w-4 h-4 ${active ? 'text-ocean-400' : 'text-white/40'}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.8} d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.066 2.573c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.573 1.066c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.066-2.573c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.8} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
    </svg>
  );
}

function HomeIcon({ active }: { active: boolean }) {
  return (
    <svg className={`w-4 h-4 ${active ? 'text-ocean-400' : 'text-white/40'}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.8} d="M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-6 0a1 1 0 001-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 001 1m-6 0h6" />
    </svg>
  );
}

function MonitorIcon({ active }: { active: boolean }) {
  return (
    <svg className={`w-4 h-4 ${active ? 'text-ocean-400' : 'text-white/40'}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.8} d="M9 17v-2m3 2v-4m3 4v-6m2 10H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
    </svg>
  );
}

function NotificationsIcon({ active }: { active: boolean }) {
  return (
    <svg className={`w-4 h-4 ${active ? 'text-ocean-400' : 'text-white/40'}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.8} d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9" />
    </svg>
  );
}

