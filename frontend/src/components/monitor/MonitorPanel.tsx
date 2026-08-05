import { useState, useEffect, useCallback } from 'react';
import { useApp } from '../../state/AppContext';
import { useT } from '../../lib/i18n/I18nContext';
import * as api from '../../services/wails';
import type { MonitorRunSummary } from '../../services/wails';

export function MonitorPanel() {
  const { dispatch } = useApp();
  const { t } = useT();
  const [runs, setRuns] = useState<MonitorRunSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [command, setCommand] = useState('');
  const [timeout, setTimeout_] = useState(300);
  const [output, setOutput] = useState<Record<string, string>>({});
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const result = await api.listMonitorRuns();
      setRuns(result?.runs ?? []);
      setError('');
    } catch (e: any) {
      setError(e.message || 'Failed to load monitor runs');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const handleStart = async () => {
    if (!command.trim()) return;
    try {
      await api.startMonitorRun(command.trim(), timeout);
      setCommand('');
      load();
    } catch (e: any) {
      setError(e.message || 'Start failed');
    }
  };

  const handleStop = async (runID: string) => {
    try { await api.stopMonitorRun(runID); load(); } catch (_) {}
  };

  const handleRead = async (runID: string) => {
    try {
      const out = await api.readMonitorOutput(runID);
      setOutput(prev => ({ ...prev, [runID]: out || '' }));
      setExpanded(prev => ({ ...prev, [runID]: !prev[runID] }));
    } catch (_) {}
  };

  const statusColor = (s: string) =>
    s === 'running' ? 'bg-yellow-500' :
    s === 'completed' ? 'bg-green-500' :
    s === 'failed' || s === 'timeout' ? 'bg-red-500' : 'bg-gray-500';

  return (
    <div className="flex-1 flex flex-col min-h-0">
      {/* Header */}
      <header className="flex items-center gap-4 px-6 py-3 border-b border-surface-border glass-surface shrink-0">
        <button className="btn-ghost !p-1.5" onClick={() => dispatch({ type: 'TOGGLE_SIDEBAR' })}>
          <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
          </svg>
        </button>
        <h1 className="text-sm font-semibold text-white/90">{t('monitor.title')}</h1>
        <button onClick={load} className="btn-ghost ml-auto !px-2 !py-1 text-xs text-white/40 hover:text-white/70" disabled={loading}>
          {loading ? t('monitor.loading') : t('monitor.refresh')}
        </button>
      </header>

      <div className="flex-1 overflow-y-auto p-6">
        <div className="max-w-3xl xl:max-w-4xl 2xl:max-w-5xl mx-auto space-y-4 animate-fade-in">
          {/* Error toast */}
          {error && (
            <div className="flex items-center gap-2 px-3 py-2 rounded bg-coral-500/10 border border-coral-500/20">
              <span className="text-xs text-coral-400 flex-1">{error}</span>
              <button onClick={() => setError('')} className="text-coral-400 hover:text-coral-300">
                <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            </div>
          )}

          {/* Command input */}
          <div className="flex gap-3 items-end">
            <div className="flex-1">
              <label className="text-xs text-white/40 block mb-1">{t('monitor.command')}</label>
              <input
                type="text" value={command} onChange={e => setCommand(e.target.value)}
                placeholder={t('monitor.commandPlaceholder')}
                className="input-field"
                onKeyDown={e => e.key === 'Enter' && handleStart()}
              />
            </div>
            <div className="w-24">
              <label className="text-xs text-white/40 block mb-1">{t('monitor.timeout')}</label>
              <input
                type="number" value={timeout} onChange={e => setTimeout_(Number(e.target.value) || 300)}
                className="input-field"
              />
            </div>
            <button
              onClick={handleStart}
              disabled={!command.trim()}
              className="btn-primary"
            >
              {t('monitor.start')}
            </button>
          </div>

          {/* Content */}
          {loading ? (
            <div className="flex items-center justify-center py-16">
              <div className="w-5 h-5 border-2 border-ocean-400/30 border-t-ocean-400 rounded-full animate-spin" />
            </div>
          ) : runs.length === 0 ? (
            <div className="text-center text-white/25 py-16">
              <svg className="w-12 h-12 mx-auto mb-4 opacity-30" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M9 17v-2m3 2v-4m3 4v-6m2 10H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
              </svg>
              <p className="text-sm">{t('monitor.emptyHint')}</p>
            </div>
          ) : (
            <ul className="space-y-2">
              {runs.map(r => (
                <li key={r.id} className="card p-3">
                  <div className="flex items-center gap-2">
                    <span className={`w-2 h-2 rounded-full shrink-0 ${statusColor(r.status)}`} />
                    <span className="font-mono text-sm text-white/80 truncate flex-1">{r.command}</span>
                    <span className="text-white/40 text-xs">{r.status}</span>
                    {r.status === 'running' && (
                      <button onClick={() => handleStop(r.id)} className="text-xs text-coral-400 hover:text-coral-300 px-2 py-0.5 rounded border border-coral-500/30">{t('monitor.stop')}</button>
                    )}
                    {(r.status === 'completed' || r.status === 'failed') && (
                      <button onClick={() => handleRead(r.id)} className="text-xs text-ocean-400 hover:text-ocean-300 px-2 py-0.5 rounded border border-ocean-500/30">
                        {expanded[r.id] ? t('monitor.hide') : t('monitor.output')}
                      </button>
                    )}
                  </div>
                  {r.error && <p className="text-coral-400 text-xs mt-1">{r.error}</p>}
                  {r.exit_code !== 0 && r.status !== 'running' && (
                    <span className="text-white/30 text-xs">{t('monitor.exitCode', { code: String(r.exit_code) })}</span>
                  )}
                  {expanded[r.id] && output[r.id] && (
                    <pre className="mt-2 bg-black/30 rounded p-2 text-xs text-white/60 font-mono max-h-48 overflow-y-auto">{output[r.id]}</pre>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  );
}
