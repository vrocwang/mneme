import { useState, useEffect } from 'react';
import * as api from '../../services/wails';
import { useT } from '../../lib/i18n/I18nContext';

export function CompanionSettings() {
  const { t } = useT();
  const [status, setStatus] = useState('');
  const [loading, setLoading] = useState(false);
  const [running, setRunning] = useState(false);
  const [screenIntelRunning, setScreenIntelRunning] = useState(false);
  const [engines, setEngines] = useState({ stt: '—', tts: '—' });
  const [interval, setInterval_] = useState(300);

  useEffect(() => {
    api.getVoiceEngines().then(e => { if (e) setEngines(e); }).catch(() => {});
  }, []);

  const activate = async () => {
    setLoading(true);
    try {
      const msg = await api.activateCompanion();
      setStatus(msg);
      if (msg && !msg.includes('not available')) setRunning(true);
    } catch (e: any) {
      setStatus(`${t('companion.error')}: ${e.message || e}`);
    } finally {
      setLoading(false);
    }
  };

  const startLoop = async () => {
    const msg = await api.startCompanionLoop();
    setStatus(msg);
    if (msg && !msg.includes('not available')) setRunning(true);
  };

  const stopLoop = () => {
    api.stopCompanionLoop();
    setRunning(false);
    setStatus(t('companion.stopped'));
  };

  const toggleScreenIntel = async () => {
    if (screenIntelRunning) {
      const msg = await api.stopScreenIntel();
      setStatus(msg);
      setScreenIntelRunning(false);
    } else {
      const msg = await api.startScreenIntel();
      setStatus(msg);
      if (msg && !msg.includes('not available')) setScreenIntelRunning(true);
    }
  };

  return (
    <div className="max-w-2xl xl:max-w-3xl 2xl:max-w-4xl animate-fade-in space-y-6">
      {/* Voice engines */}
      <section className="card">
        <div className="flex items-center gap-4 text-sm">
          <span className="text-white/50">STT:</span>
          <span className="text-white/80 font-mono">{engines.stt}</span>
          <span className="text-white/30 mx-2">|</span>
          <span className="text-white/50">TTS:</span>
          <span className="text-white/80 font-mono">{engines.tts}</span>
        </div>
      </section>

      {/* Companion loop */}
      <section>
        <h3 className="text-sm font-semibold text-white/80 mb-3">{t('companion.title')}</h3>
        <p className="text-xs text-white/30 mb-4">{t('companion.description')}</p>

        <div className="flex flex-wrap gap-2">
          <button onClick={activate} disabled={loading}
            className="px-4 py-2 bg-ocean-600 hover:bg-ocean-500 disabled:bg-gray-700 disabled:text-white/20 text-white rounded-lg text-sm font-medium transition-colors">
            {loading ? t('companion.activating') : t('companion.activate')}
          </button>
          {!running ? (
            <button onClick={startLoop}
              className="px-4 py-2 bg-ocean-500/20 text-ocean-400 hover:bg-ocean-500/30 rounded-lg text-sm transition-colors">
              {t('companion.startLoop')}
            </button>
          ) : (
            <button onClick={stopLoop}
              className="px-4 py-2 bg-coral-500/20 text-coral-400 hover:bg-coral-500/30 rounded-lg text-sm transition-colors">
              {t('companion.stopLoop')}
            </button>
          )}
        </div>
      </section>

      {/* Screen intelligence */}
      <section>
        <h3 className="text-sm font-semibold text-white/80 mb-3">{t('companion.screenIntel')}</h3>
        <p className="text-xs text-white/30 mb-4">{t('companion.screenIntelDesc')}</p>

        <div className="flex items-center gap-3 mb-3">
          <label className="text-xs text-white/40">{t('companion.intervalLabel')}</label>
          <input type="number" value={interval} onChange={e => setInterval_(Number(e.target.value) || 60)}
            className="w-24 bg-surface border border-surface-border rounded px-2 py-1 text-xs text-white/80" />
        </div>

        <div className="flex gap-2">
          <button onClick={() => api.startScreenIntelligence(interval).then(setStatus)}
            className="px-4 py-2 bg-ocean-500/20 text-ocean-400 hover:bg-ocean-500/30 rounded-lg text-sm transition-colors">
            {t('companion.startPeriodic')}
          </button>
          <button onClick={toggleScreenIntel}
            className={`px-4 py-2 rounded-lg text-sm transition-colors ${
              screenIntelRunning ? 'bg-coral-500/20 text-coral-400 hover:bg-coral-500/30' : 'bg-ocean-500/20 text-ocean-400 hover:bg-ocean-500/30'
            }`}>
            {screenIntelRunning ? t('companion.stopScreenIntel') : t('companion.startScreenIntel')}
          </button>
        </div>
      </section>

      {status && (
        <p className={`text-xs ${status.includes('not available') || status.includes('Error') ? 'text-coral-400' : 'text-sage-400'}`}>{status}</p>
      )}

      {/* Requirements */}
      <section className="card text-xs text-white/30 space-y-1.5">
        <p><strong className="text-white/50">{t('companion.requirements')}:</strong></p>
        <ul className="list-disc pl-4 space-y-0.5">
          <li>{t('companion.reqProvider')}</li>
          <li>{t('companion.reqMic')}</li>
          <li>{t('companion.reqMacCliclick')}</li>
          <li>{t('companion.reqMacPerm')}</li>
          <li>{t('companion.reqLinux')}</li>
        </ul>
      </section>
    </div>
  );
}
