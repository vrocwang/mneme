import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useT } from '../../lib/i18n/I18nContext';
import { getAppStateSnapshot, AppStateSnapshot } from '../../services/wails';
import { ROUTES } from '../../lib/routes';
import { BRAND_MARK } from '../../lib/brand';

export function HomePage() {
  const { t } = useT();
  const navigate = useNavigate();
  const [snapshot, setSnapshot] = useState<AppStateSnapshot | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    getAppStateSnapshot()
      .then(s => { setSnapshot(s); setLoading(false); })
      .catch(() => { setLoading(false); });
  }, []);

  const quickActions = [
    {
      label: t('home.newChat'),
      desc: t('home.newChatDesc'),
      path: ROUTES.chat,
      icon: ChatIcon, color: 'bg-ocean-500/10 text-ocean-400 border-ocean-500/20',
    },
    {
      label: t('home.searchMemory'),
      desc: t('home.searchMemoryDesc'),
      path: ROUTES.memory,
      icon: MemoryIcon, color: 'bg-sage-500/10 text-sage-400 border-sage-500/20',
    },
    {
      label: t('home.manageTools'),
      desc: t('home.manageToolsDesc'),
      path: ROUTES.capabilities,
      icon: ToolsIcon, color: 'bg-amber-500/10 text-amber-400 border-amber-500/20',
    },
  ];

  return (
    <div className="h-full flex items-center justify-center">
      <div className="w-full max-w-2xl xl:max-w-3xl 2xl:max-w-4xl px-6">
        {/* Hero */}
        <div className="text-center mb-12">
          <div className="w-16 h-16 rounded-2xl bg-gradient-to-br from-ocean-400 to-ocean-600 flex items-center justify-center text-white font-bold text-2xl mx-auto mb-5">
            {BRAND_MARK}
          </div>
          <h1 className="text-2xl font-bold text-white/90 mb-2">{t('home.welcome')}</h1>
          <p className="text-sm text-white/40 max-w-md mx-auto">
            {t('home.tagline')}
          </p>
        </div>

        {/* Quick actions */}
        <div className="grid grid-cols-3 gap-3 mb-8">
          {quickActions.map(action => (
            <button
              key={action.path}
              onClick={() => {
                navigate(action.path);
              }}
              className={`flex flex-col items-center gap-3 p-5 rounded-xl border ${action.color} hover:scale-[1.02] active:scale-[0.98] transition-transform text-center`}
            >
              <action.icon />
              <div>
                <div className="font-medium text-sm text-white/80">{action.label}</div>
                <div className="text-xs text-white/30 mt-0.5">{action.desc}</div>
              </div>
            </button>
          ))}
        </div>

        {/* Status bar */}
        {!loading && (
          <div className="flex items-center justify-center gap-6 text-xs text-white/30">
            <span className="flex items-center gap-1.5">
              <span className={`w-2 h-2 rounded-full ${snapshot?.provider_ready ? 'bg-sage-400' : 'bg-coral-400'}`} />
              {t('home.providerConnected')}
            </span>
            <span className="flex items-center gap-1.5">
              <span className={`w-2 h-2 rounded-full ${snapshot?.db_ready ? 'bg-sage-400' : 'bg-coral-400'}`} />
              {t('home.dbStatus')}
            </span>
            <span>{t('capabilities.toolsCount', { count: snapshot?.tool_count ?? 0 })}</span>
            <span>{t('capabilities.agentsCount', { count: snapshot?.agent_count ?? 0 })}</span>
          </div>
        )}
      </div>
    </div>
  );
}

function ChatIcon() {
  return (
    <svg className="w-7 h-7 text-ocean-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.6} d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
    </svg>
  );
}

function MemoryIcon() {
  return (
    <svg className="w-7 h-7 text-sage-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.6} d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4" />
    </svg>
  );
}

function ToolsIcon() {
  return (
    <svg className="w-7 h-7 text-amber-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.6} d="M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zm10 0a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zm10 0a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z" />
    </svg>
  );
}
