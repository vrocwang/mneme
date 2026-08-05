import { useState, useEffect, useCallback } from 'react';
import * as api from '../../services/wails';
import { useT } from '../../lib/i18n/I18nContext';
import type { VoiceConfig } from '../../services/wails';

const STT_PROVIDERS = [
  { value: 'system', key: 'voice.sttSystem' },
  { value: 'whisper', key: 'voice.sttWhisper' },
  { value: 'openai', key: 'voice.sttOpenAI' },
];

const TTS_PROVIDERS = [
  { value: 'system', key: 'voice.ttsSystem' },
  { value: 'piper', key: 'voice.ttsPiper' },
  { value: 'openai', key: 'voice.ttsOpenAI' },
];

export function VoiceSettings() {
  const { t } = useT();
  const [config, setConfig] = useState<VoiceConfig>({
    stt_provider: 'system',
    stt_model: '',
    stt_endpoint: '',
    stt_api_key: '',
    tts_provider: 'system',
    tts_model: '',
    tts_endpoint: '',
    tts_api_key: '',
  });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [msg, setMsg] = useState('');

  const flash = (text: string) => { setMsg(text); setTimeout(() => setMsg(''), 3000); };

  const load = useCallback(async () => {
    try {
      const cfg = await api.getVoiceConfig();
      setConfig(cfg);
    } catch { /* ignore */ }
    finally { setLoading(false); }
  }, []);

  useEffect(() => { load(); }, [load]);

  const save = async () => {
    setSaving(true);
    try {
      await api.setVoiceConfig(config);
      flash(t('settings.save'));
    } catch (e) {
      flash(`${t('errors.somethingWrong')}: ${e}`);
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return <div className="animate-pulse space-y-3">{[1, 2].map(i => <div key={i} className="h-24 bg-surface-muted rounded" />)}</div>;
  }

  return (
    <div className="max-w-2xl xl:max-w-3xl 2xl:max-w-4xl animate-fade-in space-y-4">
      <h2 className="text-lg font-semibold text-white/80">{t('voice.title')}</h2>

      {/* STT Section */}
      <div className="card space-y-3">
        <h3 className="text-sm font-semibold text-white/60">{t('voice.sttSection')}</h3>
        <div className="space-y-2">
          <label className="text-xs text-white/40">{t('voice.engine')}</label>
          <select
            value={config.stt_provider || 'system'}
            onChange={e => setConfig(prev => ({ ...prev, stt_provider: e.target.value }))}
            className="input-field"
          >
            {STT_PROVIDERS.map(p => (
              <option key={p.value} value={p.value}>{t(p.key)}</option>
            ))}
          </select>
        </div>
        {(config.stt_provider === 'whisper' || config.stt_provider === 'openai') && (
          <div className="space-y-2">
            <label className="text-xs text-white/40">{t('voice.model')}</label>
            <input
              value={config.stt_model || ''}
              onChange={e => setConfig(prev => ({ ...prev, stt_model: e.target.value }))}
              placeholder={config.stt_provider === 'openai' ? 'whisper-1' : 'base'}
              className="input-field"
            />
          </div>
        )}
        {config.stt_provider === 'openai' && (
          <>
            <div className="space-y-2">
              <label className="text-xs text-white/40">{t('voice.endpoint')}</label>
              <input
                value={config.stt_endpoint || ''}
                onChange={e => setConfig(prev => ({ ...prev, stt_endpoint: e.target.value }))}
                placeholder="https://api.openai.com/v1"
                className="input-field"
              />
            </div>
            <div className="space-y-2">
              <label className="text-xs text-white/40">{t('voice.apiKey')}</label>
              <input
                type="password"
                value={config.stt_api_key || ''}
                onChange={e => setConfig(prev => ({ ...prev, stt_api_key: e.target.value }))}
                placeholder="sk-..."
                className="input-field"
              />
              <p className="text-[10px] text-white/20">{t('voice.apiKeyHint')}</p>
            </div>
          </>
        )}
      </div>

      {/* TTS Section */}
      <div className="card space-y-3">
        <h3 className="text-sm font-semibold text-white/60">{t('voice.ttsSection')}</h3>
        <div className="space-y-2">
          <label className="text-xs text-white/40">{t('voice.engine')}</label>
          <select
            value={config.tts_provider || 'system'}
            onChange={e => setConfig(prev => ({ ...prev, tts_provider: e.target.value }))}
            className="input-field"
          >
            {TTS_PROVIDERS.map(p => (
              <option key={p.value} value={p.value}>{t(p.key)}</option>
            ))}
          </select>
        </div>
        {(config.tts_provider === 'piper' || config.tts_provider === 'openai') && (
          <div className="space-y-2">
            <label className="text-xs text-white/40">{config.tts_provider === 'piper' ? t('voice.voice') : t('voice.modelVoice')}</label>
            <input
              value={config.tts_model || ''}
              onChange={e => setConfig(prev => ({ ...prev, tts_model: e.target.value }))}
              placeholder={config.tts_provider === 'openai' ? 'tts-1:alloy' : 'en_US-lessac-medium'}
              className="input-field"
            />
          </div>
        )}
        {config.tts_provider === 'openai' && (
          <>
            <div className="space-y-2">
              <label className="text-xs text-white/40">{t('voice.endpoint')}</label>
              <input
                value={config.tts_endpoint || ''}
                onChange={e => setConfig(prev => ({ ...prev, tts_endpoint: e.target.value }))}
                placeholder="https://api.openai.com/v1"
                className="input-field"
              />
            </div>
            <div className="space-y-2">
              <label className="text-xs text-white/40">{t('voice.apiKey')}</label>
              <input
                type="password"
                value={config.tts_api_key || ''}
                onChange={e => setConfig(prev => ({ ...prev, tts_api_key: e.target.value }))}
                placeholder="sk-..."
                className="input-field"
              />
              <p className="text-[10px] text-white/20">{t('voice.apiKeyHint')}</p>
            </div>
          </>
        )}
      </div>

      {msg && (
        <div className={`text-xs ${msg.includes(t('errors.somethingWrong')) ? 'text-coral-400' : 'text-sage-400'} animate-fade-in`}>
          {msg}
        </div>
      )}

      <button className="btn-primary text-sm" onClick={save} disabled={saving}>
        {saving ? t('settings.saving') : t('settings.save')}
      </button>
    </div>
  );
}
