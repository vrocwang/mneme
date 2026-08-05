import { useState, useEffect, useCallback } from 'react';
import * as api from '../../services/wails';
import { useT } from '../../lib/i18n/I18nContext';
import type { TunnelInfo } from '../../services/wails';

export function WebhookSettings() {
  const { t } = useT();
  const [tunnels, setTunnels] = useState<TunnelInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState({ target: 'echo', target_id: '', description: '' });
  const [msg, setMsg] = useState('');
  const [activities, setActivities] = useState<unknown[]>([]);
  const [showActivity, setShowActivity] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await api.listTunnels();
      setTunnels(res.tunnels || []);
    } catch { /* backend may not be ready */ }
    finally { setLoading(false); }
  }, []);

  useEffect(() => { load(); }, [load]);

  const loadActivity = async () => {
    try {
      const res = await api.listTunnelActivity(50);
      if (res?.ok) { setActivities(res.activities || []); setShowActivity(true); }
    } catch { /* no-op */ }
  };

  const handleClearActivity = async () => {
    try { await api.clearTunnelActivity(); setActivities([]); } catch { /* no-op */ }
  };

  const flash = (text: string) => { setMsg(text); setTimeout(() => setMsg(''), 3000); };

  const handleCreate = async () => {
    try {
      await api.createTunnel(form.target, form.target_id || 'default', form.description);
      setCreating(false);
      setForm({ target: 'echo', target_id: '', description: '' });
      flash(t('settings.save'));
      await load();
    } catch (e) {
      flash(`${t('errors.somethingWrong')}: ${e}`);
    }
  };

  const handleDelete = async (uuid: string) => {
    try {
      await api.deleteTunnel(uuid);
      flash(t('webhooks.deleted'));
      await load();
    } catch (e) {
      flash(`${t('errors.somethingWrong')}: ${e}`);
    }
  };

  if (loading) {
    return <div className="animate-pulse h-24 bg-surface-muted rounded" />;
  }

  return (
    <div className="max-w-2xl xl:max-w-3xl 2xl:max-w-4xl animate-fade-in space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-white/80">{t('webhooks.title')}</h2>
        <button className="btn-primary text-xs" onClick={() => setCreating(true)}>
          {t('webhooks.addTunnel')}
        </button>
      </div>

      {creating && (
        <div className="card space-y-3">
          <h3 className="text-sm font-semibold text-white/60">{t('webhooks.newTunnel')}</h3>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1">
              <label className="text-xs text-white/40">{t('webhooks.target')}</label>
              <select value={form.target} onChange={e => setForm(f => ({ ...f, target: e.target.value }))} className="input-field">
                <option value="echo">{t('webhooks.targetEcho')}</option>
                <option value="agent">{t('webhooks.targetAgent')}</option>
              </select>
            </div>
            <div className="space-y-1">
              <label className="text-xs text-white/40">{t('webhooks.targetId')}</label>
              <input value={form.target_id} onChange={e => setForm(f => ({ ...f, target_id: e.target.value }))}
                placeholder={form.target === 'agent' ? 'agent_general' : ''} className="input-field" />
            </div>
          </div>
          <div className="space-y-1">
            <label className="text-xs text-white/40">{t('webhooks.description')}</label>
            <input value={form.description} onChange={e => setForm(f => ({ ...f, description: e.target.value }))}
              placeholder={t('webhooks.descPlaceholder')} className="input-field" />
          </div>
          <div className="flex gap-2">
            <button className="btn-primary text-xs" onClick={handleCreate}>{t('settings.save')}</button>
            <button className="btn-ghost text-xs" onClick={() => setCreating(false)}>{t('settings.cancel')}</button>
          </div>
        </div>
      )}

      {msg && (
        <div className={`text-xs ${msg.includes(t('errors.somethingWrong')) ? 'text-coral-400' : 'text-sage-400'}`}>{msg}</div>
      )}

      {tunnels.length === 0 ? (
        <p className="text-sm text-white/30">{t('webhooks.noTunnels')}</p>
      ) : (
        <div className="space-y-2">
          {tunnels.map(tun => (
            <div key={tun.id} className="card space-y-2">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2 min-w-0">
                  <span className="badge bg-ocean-500/15 text-ocean-400 text-[10px]">{tun.target}</span>
                  {tun.target === 'agent' && <span className="text-xs text-white/40">{tun.target_id || 'default'}</span>}
                  {tun.description && <span className="text-xs text-white/30 truncate">{tun.description}</span>}
                </div>
                <button className="btn-ghost !text-coral-400 !text-xs shrink-0" onClick={() => handleDelete(tun.tunnel_uuid)}>
                  {t('settings.remove')}
                </button>
              </div>
              <div className="flex items-center gap-2">
                <code className="text-xs text-white/50 bg-surface-muted px-2 py-1 rounded select-all">{tun.tunnel_uuid}</code>
                <button className="btn-ghost !text-[10px] !py-0.5 !px-1.5"
                  onClick={() => { navigator.clipboard.writeText(tun.tunnel_uuid); flash(t('webhooks.copied')); }}>
                  {t('webhooks.copy')}
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      <hr className="border-white/5" />

      <div className="flex items-center gap-3">
        <button className="btn-ghost text-xs text-ocean-400" onClick={loadActivity}>
          {showActivity ? 'Refresh Activity' : 'Show Recent Activity'}
        </button>
        {activities.length > 0 && (
          <button className="btn-ghost text-xs text-coral-400" onClick={handleClearActivity}>Clear</button>
        )}
      </div>

      {showActivity && (
        <div className="space-y-1 max-h-48 overflow-y-auto">
          {activities.length === 0 ? (
            <p className="text-xs text-white/30">No recent activity.</p>
          ) : (
            activities.map((a: any, i: number) => (
              <div key={i} className="flex items-center gap-2 text-xs text-white/50 bg-surface-muted rounded px-3 py-1.5">
                <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${a.status && a.status >= 400 ? 'bg-coral-400' : 'bg-sage-400'}`} />
                <span className="font-mono text-white/30 w-16 shrink-0">{a.request_id?.slice(0, 8) || '—'}</span>
                <span className="text-white/40 w-8 shrink-0">{a.status || '—'}</span>
                <span className="truncate">{a.error || a.tunnel_uuid?.slice(0, 12) || '—'}</span>
                <span className="text-white/20 ml-auto shrink-0">{a.created_at ? new Date(a.created_at).toLocaleTimeString() : ''}</span>
              </div>
            ))
          )}
        </div>
      )}

      <p className="text-xs text-white/20">{t('webhooks.helpText')}</p>
    </div>
  );
}
