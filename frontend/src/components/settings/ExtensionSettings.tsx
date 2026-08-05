import { useState, useEffect } from 'react';
import * as api from '../../services/wails';
import { useT } from '../../lib/i18n/I18nContext';
import type { ExtensionInfo } from '../../services/wails';

export function ExtensionSettings() {
  const { t } = useT();
  const [extensions, setExtensions] = useState<ExtensionInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [installPath, setInstallPath] = useState('');
  const [installing, setInstalling] = useState(false);

  const load = () => {
    setLoading(true);
    api.listExtensions()
      .then(data => { setExtensions(data || []); setError(''); })
      .catch(e => setError(String(e)))
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(); }, []);

  const handleInstall = async () => {
    if (!installPath.trim()) return;
    setInstalling(true);
    try {
      await api.installExtension(installPath.trim());
      setInstallPath('');
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Install failed');
    } finally {
      setInstalling(false);
    }
  };

  const handleUninstall = async (category: string, name: string) => {
    if (!confirm(`Uninstall ${name}?`)) return;
    try {
      await api.uninstallExtension(category, name);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Uninstall failed');
    }
  };

  const healthColor = (h: string) =>
    h === 'ok' ? 'bg-sage-400' : h === 'degraded' ? 'bg-amber-400' : h === 'error' ? 'bg-coral-400' : 'bg-gray-500';

  return (
    <div className="max-w-2xl xl:max-w-3xl 2xl:max-w-4xl animate-fade-in space-y-6">
      <section>
        <div className="flex items-center gap-2">
          <input
            className="input-field flex-1 bg-surface border border-surface-border rounded px-2.5 py-1.5 text-xs text-white/80"
            placeholder={t('extensions.installPlaceholder')}
            value={installPath}
            onChange={e => setInstallPath(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && handleInstall()}
          />
          <button
            onClick={handleInstall}
            disabled={installing || !installPath.trim()}
            className="text-xs px-3 py-1.5 rounded-md bg-ocean-500 text-white hover:bg-ocean-600 disabled:opacity-40 transition-colors shrink-0"
          >
            {installing ? t('extensions.installing') : t('extensions.install')}
          </button>
        </div>
        <p className="text-xs text-white/30 mt-1.5">{t('extensions.installHint')}</p>
      </section>

      <section>
        <h3 className="text-sm font-semibold text-white/80 mb-3">{t('extensions.installed')} ({extensions.length})</h3>
        {loading ? (
          <div className="flex items-center justify-center py-8">
            <div className="w-4 h-4 border-2 border-ocean-400/30 border-t-ocean-400 rounded-full animate-spin" />
          </div>
        ) : extensions.length === 0 ? (
          <div className="card text-xs text-white/30 py-6 text-center">{t('extensions.noExtensions')}</div>
        ) : (
          <div className="space-y-2">
            {extensions.map(ext => (
              <div key={ext.name} className="card bg-surface-elevated border border-surface-border rounded-lg px-4 py-3">
                <div className="flex items-start justify-between">
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="text-sm font-medium text-white/80">{ext.name}</span>
                      <span className="text-[10px] text-white/40">v{ext.version}</span>
                      <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-white/5 text-white/40">{ext.category}</span>
                      <span className={`w-2 h-2 rounded-full ${healthColor(ext.health)}`} title={ext.health} />
                    </div>
                    {ext.description && <p className="text-xs text-white/40 mt-1">{ext.description}</p>}
                    <div className="flex items-center gap-3 mt-1.5 text-[10px] text-white/30">
                      {ext.author && <span>{ext.author}</span>}
                      {ext.homepage && (
                        <a href={ext.homepage} target="_blank" rel="noopener noreferrer"
                          className="text-ocean-400/70 hover:text-ocean-300 transition-colors">Homepage</a>
                      )}
                      <span className={ext.loaded ? 'text-sage-400/70' : 'text-amber-400/70'}>
                        {ext.loaded ? t('extensions.loaded') : t('extensions.notLoaded')}
                      </span>
                    </div>
                  </div>
                  <button
                    onClick={() => handleUninstall(ext.category, ext.name)}
                    className="text-[10px] px-2 py-1 rounded bg-coral-500/10 text-coral-300/70 hover:bg-coral-500/20 transition-colors shrink-0 ml-3"
                  >
                    {t('extensions.remove')}
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </section>

      {error && <p className="text-xs text-coral-400">{error}</p>}

      <section className="card text-xs text-white/30 space-y-1.5">
        <p><strong className="text-white/50">{t('extensions.title')}:</strong> {t('extensions.formatHint')}</p>
        <p>{t('extensions.autoBuildHint')}</p>
      </section>
    </div>
  );
}
