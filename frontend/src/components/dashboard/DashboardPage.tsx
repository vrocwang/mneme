import { useState, useEffect, useCallback } from 'react';
import { useSelector } from 'react-redux';
import { useApp } from '../../state/AppContext';
import { useT } from '../../lib/i18n/I18nContext';
import type { RootState } from '../../store';
import * as api from '../../services/wails';

function Skeleton({ className = '' }: { className?: string }) {
  return <div className={`animate-pulse bg-surface-muted rounded ${className}`} />;
}

export function DashboardPage() {
  const { dispatch } = useApp();
  const { t } = useT();
  const threadCount = useSelector((s: RootState) => s.thread.threads.length);
  const pendingCount = useSelector((s: RootState) => s.approval.pending.length);
  const [snapshot, setSnapshot] = useState<api.AppStateSnapshot | null>(null);
  const [reflections, setReflections] = useState<unknown[]>([]);
  const [preferences, setPreferences] = useState<unknown[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchData = useCallback(async () => {
    setError(null);
    try {
      const [s, r, p] = await Promise.all([
        api.getAppStateSnapshot().catch(() => null),
        api.getReflections(10).catch(() => []),
        api.getPreferences().catch(() => []),
      ]);
      setSnapshot(s);
      setReflections((r as unknown[]) || []);
      setPreferences((p as unknown[]) || []);
    } catch (e) {
      setError(String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { fetchData(); }, [fetchData]);

  const prefCount = preferences.length;
  const metrics = [
    { label: t('dashboard.threads'), value: threadCount, color: 'text-ocean-400' },
    { label: t('dashboard.tools'), value: snapshot?.tool_count ?? '--', color: 'text-sage-400' },
    { label: t('dashboard.agents'), value: snapshot?.agent_count ?? '--', color: 'text-amber-400' },
    { label: t('dashboard.pending'), value: pendingCount, color: 'text-coral-400' },
    { label: t('dashboard.preferences'), value: prefCount, color: 'text-purple-400' },
  ];

  const statusItems = [
    { label: t('home.providerConnected', 'Provider'), ok: snapshot?.provider_ready ?? false },
    { label: t('home.dbStatus', 'Database'), ok: snapshot?.db_ready ?? false },
  ];

  return (
    <div className="flex-1 flex flex-col min-h-0">
      <header className="flex items-center justify-between px-6 py-3 border-b border-surface-border glass-surface shrink-0">
        <div className="flex items-center gap-4">
          <button className="btn-ghost !p-1.5" onClick={() => dispatch({ type: 'TOGGLE_SIDEBAR' })}>
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
            </svg>
          </button>
          <h1 className="text-sm font-semibold text-white/90">{t('dashboard.title')}</h1>
        </div>
        <button onClick={fetchData} className="text-xs text-white/30 hover:text-white/60 transition-colors">
          {t('dashboard.refresh')}
        </button>
      </header>

      <div className="flex-1 overflow-y-auto p-6">
        {loading ? (
          <div className="max-w-3xl xl:max-w-5xl 2xl:max-w-6xl space-y-6">
            <div className="grid grid-cols-5 gap-4">
              {[1, 2, 3, 4, 5].map(i => <Skeleton key={i} className="h-24 rounded-lg" />)}
            </div>
            <Skeleton className="h-32 rounded-lg" />
          </div>
        ) : error ? (
          <div className="card text-center py-12">
            <p className="text-coral-400 text-sm mb-2">{t('dashboard.failedToLoad')}</p>
            <p className="text-white/20 text-xs font-mono">{error}</p>
            <button onClick={fetchData} className="mt-3 text-xs text-ocean-400 hover:underline">{t('dashboard.retry')}</button>
          </div>
        ) : (
          <div className="max-w-3xl xl:max-w-5xl 2xl:max-w-6xl space-y-6 animate-fade-in">
            {/* Metrics cards */}
            <div className="grid grid-cols-5 gap-4">
              {metrics.map(m => (
                <div key={m.label} className="card text-center">
                  <div className={`text-2xl font-bold ${m.color}`}>{m.value}</div>
                  <div className="text-xs text-white/30 mt-1 uppercase tracking-wider">{m.label}</div>
                </div>
              ))}
            </div>

            {/* System status */}
            <div className="card">
              <h3 className="text-sm font-semibold text-white/60 mb-3">{t('home.systemStatus', 'System Status')}</h3>
              <div className="flex gap-6">
                {statusItems.map(s => (
                  <div key={s.label} className="flex items-center gap-2">
                    <div className={`w-2 h-2 rounded-full ${s.ok ? 'bg-sage-400' : 'bg-coral-400'}`} />
                    <span className="text-sm text-white/70">{s.label}</span>
                    <span className={`text-xs ${s.ok ? 'text-sage-400' : 'text-coral-400'}`}>
                      {s.ok ? t('home.connected', 'OK') : t('home.notConnected', 'Not Ready')}
                    </span>
                  </div>
                ))}
              </div>
            </div>



            {/* Reflections */}
            {reflections.length > 0 && (
              <div>
                <h3 className="text-sm font-semibold text-white/60 mb-3">{t('dashboard.reflections')}</h3>
                <div className="space-y-3">
                  {reflections.map((r: any, i: number) => (
                    <div key={i} className="card">
                      <div className="flex items-center gap-2 mb-2">
                        <span className={`badge text-xs px-2 py-0.5 rounded-full ${r.kind === 'insight' ? 'bg-ocean-500/20 text-ocean-400' : 'bg-amber-500/20 text-amber-400'}`}>
                          {r.kind || 'reflection'}
                        </span>
                      </div>
                      <p className="text-sm text-white/50">{r.content || r.summary || JSON.stringify(r)}</p>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
