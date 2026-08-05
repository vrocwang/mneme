import { useState, useEffect } from 'react';
import * as api from '../../services/wails';
import { useT } from '../../lib/i18n/I18nContext';

export function ChannelSettings() {
  const { t } = useT();
  const [channels, setChannels] = useState<api.ChannelInfo[]>([]);
  const [available, setAvailable] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    api.listChannels().then(r => {
      if (r?.ok) {
        setChannels(r.channels || []);
        setAvailable(r.available || []);
      }
    }).catch(e => setError(String(e)))
      .finally(() => setLoading(false));
  }, []);

  return (
    <div className="max-w-2xl xl:max-w-3xl 2xl:max-w-4xl animate-fade-in space-y-6">
      <section>
        <h3 className="text-sm font-semibold text-white/80 mb-3">{t('channels.configured')}</h3>
        {loading ? (
          <div className="flex items-center justify-center py-8">
            <div className="w-4 h-4 border-2 border-ocean-400/30 border-t-ocean-400 rounded-full animate-spin" />
          </div>
        ) : channels.length === 0 ? (
          <div className="card text-xs text-white/30 py-6 text-center">{t('channels.noChannels')}</div>
        ) : (
          <div className="space-y-2">
            {channels.map(ch => (
              <div key={ch.name} className="card bg-surface-elevated border border-surface-border rounded-lg px-4 py-3 flex items-center justify-between">
                <div>
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium text-white/80">{ch.name}</span>
                    <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-white/5 text-white/40">{ch.type}</span>
                  </div>
                  <p className="text-xs text-white/30 mt-0.5">{ch.enabled ? t('channels.enabled') : t('channels.disabled')}</p>
                </div>
                <button
                  onClick={async () => {
                    try {
                      if (ch.enabled) {
                        await api.disableChannel(ch.name);
                      } else {
                        await api.enableChannel(ch.name);
                      }
                      const r = await api.listChannels();
                      if (r?.ok) { setChannels(r.channels || []); }
                    } catch (e) { setError(String(e)); }
                  }}
                  className={`px-3 py-1 rounded text-xs font-medium transition-colors ${
                    ch.enabled ? 'bg-coral-500/20 text-coral-400 hover:bg-coral-500/30' : 'bg-sage-500/20 text-sage-400 hover:bg-sage-500/30'
                  }`}
                >
                  {ch.enabled ? 'Disable' : 'Enable'}
                </button>
              </div>
            ))}
          </div>
        )}
      </section>

      {available.length > 0 && (
        <section>
          <h3 className="text-sm font-semibold text-white/80 mb-3">{t('channels.available')}</h3>
          <div className="flex flex-wrap gap-2">
            {available.map(name => (
              <span key={name} className="text-xs px-2.5 py-1 rounded-full bg-ocean-500/10 text-ocean-300/70 font-mono">{name}</span>
            ))}
          </div>
          <p className="text-xs text-white/30 mt-2">
            {t('channels.configHint')} <code className="text-white/50">config.toml</code> {t('channels.under')} <code className="text-white/50">[channels.&lt;name&gt;]</code>.
            {t('channels.extensionsNote')}
          </p>
        </section>
      )}

      <section className="card text-xs text-white/30 space-y-1.5">
        <p><strong className="text-white/50">Configuration:</strong> Add a <code className="text-white/50">[channels.&lt;name&gt;]</code> block to your config.toml:</p>
        <pre className="bg-surface border border-surface-border rounded p-2 text-[11px] text-white/50 font-mono overflow-x-auto">
{`[channels.telegram]
enabled = true
token = "YOUR_BOT_TOKEN"

[channels.discord]
enabled = true
token = "YOUR_BOT_TOKEN"`}
        </pre>
        <p>Channels are loaded at startup and relay messages to the agent via the ChatService.</p>
      </section>

      {error && <p className="text-xs text-coral-400">{error}</p>}
    </div>
  );
}
