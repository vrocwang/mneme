import { useCallback, useState, useEffect, useRef } from 'react';
import * as api from '../../services/wails';
import { useT } from '../../lib/i18n/I18nContext';

export function ChannelSettings() {
  const { t } = useT();
  const [channels, setChannels] = useState<api.ChannelInfo[]>([]);
  const [available, setAvailable] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  // Channel name with a mutation in flight. Mutations are serialised: while one
  // is pending every row is disabled, so completions cannot interleave the
  // busy/error state.
  const [busy, setBusy] = useState('');
  const mutatingRef = useRef(false);

  // Reload the channel list; returns a failure message (empty on success) so
  // the caller reports it instead of rendering the empty state as the truth.
  const refresh = useCallback(async (): Promise<string> => {
    try {
      const r = await api.listChannels();
      if (!r?.ok) return t('errors.loadChannels');
      setChannels(r.channels || []);
      setAvailable(r.available || []);
      return '';
    } catch (e) {
      return String(e);
    }
  }, [t]);

  useEffect(() => {
    refresh().then(failure => { if (failure) setError(failure); }).finally(() => setLoading(false));
  }, [refresh]);

  // Enable/disable a channel, then always reload: on failure the backend rolls
  // back, so the reloaded list is the truth either way. Backend errors are
  // surfaced instead of being swallowed.
  const mutate = useCallback(async (name: string, enable: boolean) => {
    if (mutatingRef.current) return;
    mutatingRef.current = true;
    setError('');
    setBusy(name);
    let failure = '';
    try {
      const r = enable ? await api.enableChannel(name) : await api.disableChannel(name);
      if (r && r.ok === false) {
        failure = r.error || `${enable ? t('channels.enable') : t('channels.disable')} ${name} failed`;
      }
    } catch (e) {
      failure = String(e);
    }
    try {
      const refreshFailure = await refresh();
      setError(failure || refreshFailure);
    } finally {
      setBusy('');
      mutatingRef.current = false;
    }
  }, [refresh, t]);

  // Available types not configured yet — these can now be added at runtime.
  const unconfigured = available.filter(name => !channels.some(ch => ch.name === name));

  const statusText = (ch: api.ChannelInfo) => ch.enabled
    ? `${t('channels.enabled')} · ${ch.running ? t('channels.running') : t('channels.notRunning')}`
    : t('channels.disabled');

  return (
    <div className="max-w-2xl xl:max-w-3xl 2xl:max-w-4xl animate-fade-in space-y-6">
      <section>
        <h3 className="text-sm font-semibold text-white/80 mb-3">{t('channels.configured')}</h3>
        {loading ? (
          <div className="flex items-center justify-center py-8">
            <div className="w-4 h-4 border-2 border-ocean-400/30 border-t-ocean-400 rounded-full animate-spin" />
          </div>
        ) : channels.length === 0 ? (
          // A failed load must not read as "nothing configured"; the error line
          // below carries the reason.
          error ? null : <div className="card text-xs text-white/30 py-6 text-center">{t('channels.noChannels')}</div>
        ) : (
          <div className="space-y-2">
            {channels.map(ch => (
              <div key={ch.name} className="card bg-surface-elevated border border-surface-border rounded-lg px-4 py-3 flex items-center justify-between">
                <div>
                  <div className="flex items-center gap-2">
                    <span className={`w-1.5 h-1.5 rounded-full ${ch.running ? 'bg-sage-400' : 'bg-white/20'}`} />
                    <span className="text-sm font-medium text-white/80">{ch.name}</span>
                    <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-white/5 text-white/40">{ch.type}</span>
                  </div>
                  <p className={`text-xs mt-0.5 ${ch.enabled && !ch.running ? 'text-coral-400/80' : 'text-white/30'}`}>
                    {statusText(ch)}
                  </p>
                </div>
                <button
                  onClick={() => mutate(ch.name, !ch.enabled)}
                  disabled={busy !== ''}
                  className={`px-3 py-1 rounded text-xs font-medium transition-colors disabled:opacity-50 disabled:cursor-not-allowed ${
                    ch.enabled ? 'bg-coral-500/20 text-coral-400 hover:bg-coral-500/30' : 'bg-sage-500/20 text-sage-400 hover:bg-sage-500/30'
                  }`}
                >
                  {ch.enabled ? t('channels.disable') : t('channels.enable')}
                </button>
              </div>
            ))}
          </div>
        )}
      </section>

      {unconfigured.length > 0 && (
        <section>
          <h3 className="text-sm font-semibold text-white/80 mb-3">{t('channels.available')}</h3>
          <div className="flex flex-wrap gap-2">
            {unconfigured.map(name => (
              <span key={name} className="inline-flex items-center gap-2 text-xs px-2.5 py-1 rounded-full bg-ocean-500/10 text-ocean-300/70 font-mono">
                {name}
                <button
                  onClick={() => mutate(name, true)}
                  disabled={busy !== ''}
                  className="text-white/40 hover:text-white/80 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  {busy === name ? '…' : t('channels.add')}
                </button>
              </span>
            ))}
          </div>
          <p className="text-xs text-white/30 mt-2">{t('channels.addHint')}</p>
          <p className="text-xs text-white/30 mt-2">
            {t('channels.configHint')} <code className="text-white/50">config.toml</code> {t('channels.under')} <code className="text-white/50">[channels.&lt;name&gt;]</code>.
            {t('channels.extensionsNote')}
          </p>
        </section>
      )}

      <section className="card text-xs text-white/30 space-y-1.5">
        <p><strong className="text-white/50">Configuration:</strong> Add a <code className="text-white/50">[channels.&lt;name&gt;]</code> block to your config.toml (<code className="text-white/50">disabled</code> is the off switch — there is no <code className="text-white/50">enabled</code> key):</p>
        <pre className="bg-surface border border-surface-border rounded p-2 text-[11px] text-white/50 font-mono overflow-x-auto">
{`[channels.telegram]
disabled = false
token = "YOUR_BOT_TOKEN"

[channels.discord]
disabled = false
token = "YOUR_BOT_TOKEN"`}
        </pre>
        <p>Channels are loaded at startup and relay messages to the agent via the ChatService.</p>
      </section>

      {error && <p className="text-xs text-coral-400">{error}</p>}
    </div>
  );
}
