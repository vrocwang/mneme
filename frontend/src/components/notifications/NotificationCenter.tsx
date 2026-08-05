import { useState } from 'react';
import { useT } from '../../lib/i18n/I18nContext';
import { useApp } from '../../state/AppContext';
import { useApprovals } from '../../hooks/useApprovals';

interface Notification {
  id: string;
  kind: string;
  title: string;
  body: string;
  timestamp: string;
  read: boolean;
}

function KindIcon({ kind }: { kind: string }) {
  switch (kind) {
    case 'approval':
      return (
        <svg className="w-4 h-4 text-amber-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
        </svg>
      );
    case 'error':
      return (
        <svg className="w-4 h-4 text-coral-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
      );
    case 'warning':
      return (
        <svg className="w-4 h-4 text-amber-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-2.694-.833-3.464 0L3.34 16.5c-.77.833.192 2.5 1.732 2.5z" />
        </svg>
      );
    case 'success':
      return (
        <svg className="w-4 h-4 text-sage-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
      );
    default:
      return (
        <svg className="w-4 h-4 text-white/30" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
      );
  }
}

export function NotificationCenter() {
  const { t } = useT();
  const { state } = useApp();
  const { pendingApprovals, decideApproval } = useApprovals();
  const [filter, setFilter] = useState<'all' | 'unread'>('all');
  const [readIds, setReadIds] = useState<Set<string>>(new Set());

  const markAllRead = () => {
    const allIds = new Set([
      ...pendingApprovals.map(a => a.id),
      ...state.toasts.map(t => t.id),
    ]);
    setReadIds(allIds);
  };

  const notifications: Notification[] = [
    ...pendingApprovals.map(a => ({
      id: a.id,
      kind: 'approval',
      title: t('notifications.approvalRequest'),
      body: `${a.tool_name}: ${a.reason || ''}`,
      timestamp: a.created_at || '',
      read: readIds.has(a.id),
    })),
    ...state.toasts.map(toast => ({
      id: toast.id,
      kind: toast.kind,
      title: toast.title,
      body: toast.body,
      timestamp: new Date().toISOString(),
      read: readIds.has(toast.id),
    })),
  ];

  const unreadCount = notifications.filter(n => !n.read).length;
  const filtered = filter === 'unread'
    ? notifications.filter(n => !n.read)
    : notifications;

  return (
    <div className="flex-1 flex flex-col min-h-0">
      <header className="flex items-center gap-4 px-6 py-3 border-b border-surface-border glass-surface shrink-0">
        <h1 className="text-sm font-semibold text-white/90">{t('notifications.title')}</h1>
        <div className="flex items-center gap-2 ml-auto">
          {unreadCount > 0 && (
            <button onClick={markAllRead} className="text-xs text-white/30 hover:text-white/60 transition-colors">
              {t('notifications.markAllRead')}
            </button>
          )}
          <button
            onClick={() => setFilter('all')}
            className={`px-2.5 py-0.5 text-xs rounded transition-colors ${filter === 'all' ? 'bg-ocean-500/20 text-ocean-400' : 'text-white/40 hover:text-white/60'}`}
          >
            {t('notifications.all')}
          </button>
          <button
            onClick={() => setFilter('unread')}
            className={`px-2.5 py-0.5 text-xs rounded transition-colors ${filter === 'unread' ? 'bg-ocean-500/20 text-ocean-400' : 'text-white/40 hover:text-white/60'}`}
          >
            {t('notifications.unread')}{unreadCount > 0 && ` (${unreadCount})`}
          </button>
        </div>
      </header>

      <div className="flex-1 overflow-y-auto p-6">
        <div className="max-w-2xl xl:max-w-3xl 2xl:max-w-4xl mx-auto">
          {filtered.length === 0 ? (
            <div className="text-center py-16 text-white/20">
              <svg className="w-10 h-10 mx-auto mb-3 opacity-20" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9" />
              </svg>
              <p className="text-sm">{filter === 'unread' ? t('notifications.noUnread') : t('notifications.empty')}</p>
            </div>
          ) : (
            <div className="space-y-2 animate-fade-in">
              {filtered.map(n => (
                <div
                  key={n.id}
                  className={`flex items-start gap-3 p-4 rounded-xl border transition-colors ${
                    n.read ? 'bg-surface border-surface-border opacity-60' : 'bg-surface-overlay border-ocean-500/20'
                  }`}
                >
                  <span className="shrink-0 mt-0.5"><KindIcon kind={n.kind} /></span>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium text-white/90">{n.title}</span>
                      {!n.read && <span className="w-2 h-2 rounded-full bg-ocean-400 shrink-0" />}
                    </div>
                    <p className="text-xs text-white/50 mt-1 line-clamp-2">{n.body}</p>
                    {n.kind === 'approval' && (
                      <div className="flex gap-1.5 mt-2">
                        <button
                          className="text-[10px] px-1.5 py-0.5 bg-sage-500/20 text-sage-400 rounded hover:bg-sage-500/30 transition-colors"
                          onClick={() => decideApproval(n.id, 'approve_once')}
                        >
                          {t('approval.approveOnce')}
                        </button>
                        <button
                          className="text-[10px] px-1.5 py-0.5 bg-coral-500/20 text-coral-400 rounded hover:bg-coral-500/30 transition-colors"
                          onClick={() => decideApproval(n.id, 'deny')}
                        >
                          {t('approval.deny')}
                        </button>
                      </div>
                    )}
                  </div>
                  <span className="text-[10px] text-white/20 shrink-0 whitespace-nowrap">
                    {n.timestamp ? new Date(n.timestamp).toLocaleTimeString() : ''}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
