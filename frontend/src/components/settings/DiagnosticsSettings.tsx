import { useState, useEffect, useCallback } from 'react';
import * as api from '../../services/wails';
import { useT } from '../../lib/i18n/I18nContext';
import type { ToolDiagnostics, DoctorReport, CostDashboard, AppStateSnapshot } from '../../services/wails';

function StatRow({ label, value, sub }: { label: string; value: string | number; sub?: string }) {
  return (
    <div className="flex items-center justify-between py-2 border-b border-surface-border last:border-b-0">
      <span className="text-sm text-white/70">{label}</span>
      <div className="text-right">
        <span className="text-sm font-mono text-white/90">{value}</span>
        {sub && <span className="text-xs text-white/30 block">{sub}</span>}
      </div>
    </div>
  );
}

function StatusBadge({ ok }: { ok: boolean }) {
  return (
    <span className={`inline-block w-2.5 h-2.5 rounded-full ${ok ? 'bg-sage-400' : 'bg-coral-400'} mr-1.5`} />
  );
}

export function DiagnosticsSettings() {
  const { t } = useT();
  const [tools, setTools] = useState<ToolDiagnostics | null>(null);
  const [doctor, setDoctor] = useState<DoctorReport | null>(null);
  const [cost, setCost] = useState<CostDashboard | null>(null);
  const [snapshot, setSnapshot] = useState<AppStateSnapshot | null>(null);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    setLoading(true);
    const [t, d, c, s] = await Promise.allSettled([
      api.getToolDiagnostics(),
      api.getDoctorReport(),
      api.getCostDashboard(),
      api.getAppStateSnapshot(),
    ]);
    if (t.status === 'fulfilled') setTools(t.value);
    if (d.status === 'fulfilled') setDoctor(d.value);
    if (c.status === 'fulfilled') setCost(c.value);
    if (s.status === 'fulfilled') setSnapshot(s.value);
    setLoading(false);
  }, []);

  useEffect(() => { load(); }, [load]);

  if (loading) {
    return (
      <div className="animate-pulse space-y-4">
        {[1, 2, 3, 4].map(i => <div key={i} className="h-24 bg-surface-muted rounded-lg" />)}
      </div>
    );
  }

  return (
    <div className="max-w-2xl xl:max-w-3xl 2xl:max-w-4xl animate-fade-in space-y-6">
      {/* System snapshot */}
      {snapshot?.ok && (
        <div className="card space-y-1">
          <h3 className="text-sm font-semibold text-white/80 mb-2">{t('diagnostics.title')}</h3>
          <StatRow label={t('diagnostics.totalTools')} value={snapshot.tool_count} />
          <StatRow label="Agents" value={snapshot.agent_count} />
          <StatRow label={t('approval.pending')} value={snapshot.pending_approvals} />
          <StatRow label="Provider" value={snapshot.provider_ready ? 'Ready' : 'Not configured'} />
          <StatRow label="Database" value={snapshot.db_ready ? 'Connected' : 'Not available'} />
        </div>
      )}

      {/* Tool registry */}
      {tools?.ok && (
        <div className="card space-y-1">
          <h3 className="text-sm font-semibold text-white/80 mb-2">Tools</h3>
          <StatRow label={t('diagnostics.totalTools')} value={tools.totalTools} />
          <StatRow label={t('diagnostics.enabledTools')} value={tools.enabledTools} />
          <StatRow label={t('diagnostics.writeSurfaces')} value={tools.writeSurfaces} sub="tools with side effects" />
          <StatRow label="In-process" value={tools.inProcessTools} />
          <StatRow label="MCP stdio" value={tools.mcpStdioTools} />
          <StatRow label="JSON-RPC" value={tools.jsonRpcTools} />
          {tools.bySource && Object.keys(tools.bySource).length > 0 && (
            <div className="mt-2 pt-2 border-t border-surface-border">
              <span className="text-xs text-white/40">By source:</span>
              {Object.entries(tools.bySource).map(([src, count]) => (
                <StatRow key={src} label={`  ${src}`} value={count} />
              ))}
            </div>
          )}
          {tools.recentDenials && tools.recentDenials.length > 0 && (
            <div className="mt-3">
              <span className="text-xs font-semibold text-coral-400">{t('diagnostics.recentDenials')}</span>
              <div className="mt-1 space-y-1 max-h-32 overflow-y-auto">
                {tools.recentDenials.map((d, i) => (
                  <div key={i} className="text-xs text-white/40 bg-surface-muted rounded px-2 py-1 font-mono">
                    <span className="text-coral-400/70">{d.tool_name}</span> — {d.reason}
                    <span className="text-white/20 ml-2">{new Date(d.time).toLocaleTimeString()}</span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      {/* Doctor report */}
      {doctor?.ok && (
        <div className="card space-y-2">
          <h3 className="text-sm font-semibold text-white/80 mb-1">Health Checks</h3>
          {doctor.checks.map((check, i) => (
            <div key={i} className="flex items-center gap-2 text-sm">
              <StatusBadge ok={check.status === 'ok'} />
              <span className="text-white/60">{check.name}</span>
              <span className="text-white/30 text-xs flex-1 text-right truncate">{check.message}</span>
            </div>
          ))}
        </div>
      )}

      {/* Cost */}
      {cost?.ok && (
        <div className="card space-y-1">
          <h3 className="text-sm font-semibold text-white/80 mb-2">Cost</h3>
          <StatRow label="Total cost" value={`$${(cost.total_cost_cents / 100).toFixed(2)}`} />
          <StatRow label="Budget used" value={`${cost.budget_used_pct.toFixed(1)}%`} />
          <p className="text-xs text-white/30 mt-1">{cost.overview}</p>
        </div>
      )}

      <button className="btn-ghost text-xs mt-2" onClick={load}>
        {t('keyring.retryProbe')}
      </button>
    </div>
  );
}
