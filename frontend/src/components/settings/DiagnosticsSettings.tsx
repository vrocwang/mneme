import { useState, useEffect, useCallback } from 'react';
import * as api from '../../services/wails';
import { useT } from '../../lib/i18n/I18nContext';
import type { ToolDiagnostics, DoctorReport, CostDashboard, AppStateSnapshot, AuditEventRecord } from '../../services/wails';

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
  const [denials, setDenials] = useState<AuditEventRecord[]>([]);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    setLoading(true);
    const [t, d, c, s, dn] = await Promise.allSettled([
      api.getToolDiagnostics(),
      api.getDoctorReport(),
      api.getCostDashboard(),
      api.getAppStateSnapshot(),
      // Tool denials come from the audit trail now that the diagnostics payload
      // no longer carries them. "tool.blocked" is security.AuditToolBlocked.
      api.listAuditEventsByKind('tool.blocked', 10),
    ]);
    if (t.status === 'fulfilled') setTools(t.value);
    if (d.status === 'fulfilled') setDoctor(d.value);
    if (c.status === 'fulfilled') setCost(c.value);
    if (s.status === 'fulfilled') setSnapshot(s.value);
    if (dn.status === 'fulfilled') setDenials(dn.value || []);
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
          <StatRow label={t('capabilities.agents')} value={snapshot.agent_count} />
          <StatRow label={t('approval.pending')} value={snapshot.pending_approvals} />
          <StatRow label={t('home.providerConnected')}
            value={snapshot.provider_ready ? t('diagnostics.providerReady') : t('diagnostics.providerNotConfigured')} />
          <StatRow label={t('home.dbStatus')}
            value={snapshot.db_ready ? t('home.connected') : t('diagnostics.notAvailable')} />
        </div>
      )}

      {/* Tool registry */}
      {tools?.ok && (
        <div className="card space-y-1">
          <h3 className="text-sm font-semibold text-white/80 mb-2">{t('capabilities.tools')}</h3>
          <StatRow label={t('diagnostics.totalTools')} value={tools.totalTools} />
          <StatRow label={t('diagnostics.enabledTools')} value={tools.enabledTools} />
          <StatRow label={t('diagnostics.writeSurfaces')} value={tools.writeSurfaces} sub={t('diagnostics.sideEffectsSub')} />
          <StatRow label={t('diagnostics.inProcess')} value={tools.inProcessTools} />
          <StatRow label={t('diagnostics.mcpStdio')} value={tools.mcpStdioTools} />
          <StatRow label={t('diagnostics.mcpHttp')} value={tools.mcpHTTPTools} />
          <StatRow label={t('diagnostics.jsonRpc')} value={tools.jsonRpcTools} />
          {tools.bySource && Object.keys(tools.bySource).length > 0 && (
            <div className="mt-2 pt-2 border-t border-surface-border">
              <span className="text-xs text-white/40">{t('diagnostics.bySource')}</span>
              {Object.entries(tools.bySource).map(([src, count]) => (
                <StatRow key={src} label={`  ${src}`} value={count} />
              ))}
            </div>
          )}
        </div>
      )}

      {/* Recent tool denials, from the security audit trail */}
      {denials.length > 0 && (
        <div className="card space-y-1">
          <h3 className="text-xs font-semibold text-coral-400 mb-1">{t('diagnostics.recentDenials')}</h3>
          <div className="space-y-1 max-h-40 overflow-y-auto">
            {denials.map(d => (
              <div key={d.id} className="text-xs text-white/40 bg-surface-muted rounded px-2 py-1 font-mono">
                <span className="text-coral-400/70">{d.tool_name || d.command || d.kind}</span> — {d.reason || d.decision || ''}
                <span className="text-white/20 ml-2">{new Date(d.created_at).toLocaleTimeString()}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Doctor report */}
      {doctor?.ok && (
        <div className="card space-y-2">
          <h3 className="text-sm font-semibold text-white/80 mb-1">{t('diagnostics.healthChecks')}</h3>
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
          <h3 className="text-sm font-semibold text-white/80 mb-2">{t('diagnostics.cost')}</h3>
          <StatRow label={t('diagnostics.totalCost')} value={`$${(cost.total_cost_cents / 100).toFixed(2)}`} />
          <StatRow label={t('diagnostics.budgetUsed')} value={`${cost.budget_used_pct.toFixed(1)}%`} />
          <p className="text-xs text-white/30 mt-1">{cost.overview}</p>
        </div>
      )}

      <button className="btn-ghost text-xs mt-2" onClick={load}>
        {t('dashboard.refresh')}
      </button>
    </div>
  );
}
