import { useState, useEffect } from 'react';
import { useT } from '../../lib/i18n/I18nContext';
import * as api from '../../services/wails';

interface Props {
  onComplete: () => void;
}

type Step = 'welcome' | 'providers' | 'workspace' | 'skills' | 'voice' | 'ready';

export function OnboardingWizard({ onComplete }: Props) {
  const { t } = useT();
  const [step, setStep] = useState<Step>('welcome');
  const [dismissed, setDismissed] = useState(false);
  const [workspace, setWorkspace] = useState('');
  const [workspaceInput, setWorkspaceInput] = useState('');
  const [workspaceChanged, setWorkspaceChanged] = useState(false);
  const [hasProviders, setHasProviders] = useState(false);
  const [providerCount, setProviderCount] = useState(0);

  // Provider form state.
  const [providerType, setProviderType] = useState('openai');
  const [apiKey, setApiKey] = useState('');
  const [baseUrl, setBaseUrl] = useState('');
  const [models, setModels] = useState('');
  const [adding, setAdding] = useState(false);
  const [added, setAdded] = useState(false);
  const [providerError, setProviderError] = useState('');
  const [workspaceMsg, setWorkspaceMsg] = useState('');
  const [applying, setApplying] = useState(false);

  const providerDefaults: Record<string, { baseUrl: string; needKey: boolean; label: string; defaultModels: string }> = {
    openai:    { baseUrl: 'https://api.openai.com/v1', needKey: true,  label: 'OpenAI-compatible (OpenAI, Groq, DeepSeek, etc.)', defaultModels: 'gpt-4o, gpt-4o-mini, gpt-4.1' },
    anthropic: { baseUrl: 'https://api.anthropic.com', needKey: true, label: 'Anthropic-compatible (Claude, Amazon Bedrock, etc.)', defaultModels: 'claude-sonnet-4-6, claude-haiku-4-5' },
    ollama:    { baseUrl: 'http://localhost:11434',    needKey: false, label: 'Ollama (local models — Llama, Mistral, Gemma, etc.)', defaultModels: 'llama3.2, mistral, gemma3' },
  };

  useEffect(() => {
    api.health().then(h => {
      const ws = String(h?.workspace || '');
      if (ws) { setWorkspace(ws); setWorkspaceInput(ws); }
    }).catch(() => {});
    api.listProviders().then(p => {
      if (Array.isArray(p) && p.length > 0) {
        setHasProviders(true);
        setProviderCount(p.length);
      }
    }).catch(() => {});
  }, []);

  if (dismissed) return null;

  const steps: { key: Step; title: string; desc: string }[] = [
    { key: 'welcome', title: t('onboarding.welcomeTitle'), desc: t('onboarding.welcomeDesc') },
    { key: 'providers', title: t('onboarding.providersTitle'), desc: t('onboarding.providersDesc') },
    { key: 'workspace', title: t('onboarding.workspaceTitle'), desc: t('onboarding.workspaceDesc') },
    { key: 'skills', title: t('onboarding.skillsTitle'), desc: t('onboarding.skillsDesc') },
    { key: 'voice', title: t('onboarding.voiceTitle'), desc: t('onboarding.voiceDesc') },
    { key: 'ready', title: t('onboarding.readyTitle'), desc: t('onboarding.readyDesc') },
  ];
  const currentIdx = steps.findIndex(s => s.key === step);

  const finish = () => {
    localStorage.setItem('onboarding_done', 'true');
    localStorage.removeItem('walkthrough_done');
    onComplete();
  };

  const dismiss = () => {
    localStorage.setItem('onboarding_done', 'true');
    localStorage.setItem('walkthrough_done', 'true');
    setDismissed(true);
  };

  const addProvider = async () => {
    const cfg = providerDefaults[providerType];
    const resolvedBaseUrl = baseUrl.trim() || cfg.baseUrl;
    if (!resolvedBaseUrl) { setProviderError('Base URL is required'); return; }
    if (cfg.needKey && !apiKey.trim()) { setProviderError('API key is required for this provider type'); return; }
    setAdding(true);
    setProviderError('');
    try {
      const name = providerType === 'openai' ? 'OpenAI' : providerType === 'anthropic' ? 'Anthropic' : 'Ollama';
      const modelList = (models.trim() || cfg.defaultModels).split(',').map(m => m.trim()).filter(Boolean);
      await api.addProvider({
        name, type: providerType, api_key: apiKey || '',
        base_url: resolvedBaseUrl, models: modelList,
      } as any);
      setAdded(true);
      setHasProviders(true);
      setProviderCount(c => c + 1);
      setApiKey('');
      setBaseUrl('');
      setModels('');
    } catch (e) {
      setProviderError(String(e));
    } finally {
      setAdding(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className="bg-surface w-full max-w-lg mx-4 rounded-2xl border border-surface-border shadow-2xl p-8 animate-fade-in">
        {/* Step indicators */}
        <div className="flex gap-2 mb-8">
          {steps.map((s, i) => (
            <div
              key={s.key}
              className={`h-1 flex-1 rounded-full transition-colors ${
                i <= currentIdx ? 'bg-ocean-500' : 'bg-surface-border'
              }`}
            />
          ))}
        </div>

        {/* Content */}
        <div className="min-h-[200px]">
          {step === 'welcome' && (
            <div className="text-center space-y-6">
              <div className="w-16 h-16 mx-auto rounded-2xl bg-gradient-to-br from-ocean-400 to-ocean-600 flex items-center justify-center">
                <svg className="w-8 h-8 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
                </svg>
              </div>
              <div>
                <h2 className="text-2xl font-bold text-white mb-2">{t('onboarding.welcomeTitle')}</h2>
                <p className="text-white/60 text-sm leading-relaxed">{t('onboarding.welcomeDesc')}</p>
              </div>
            </div>
          )}

          {step === 'providers' && (
            <div className="space-y-4">
              <h2 className="text-xl font-bold text-white mb-4">{t('onboarding.providersTitle')}</h2>
              <p className="text-white/60 text-sm mb-4">{t('onboarding.providersDesc')}</p>

              {hasProviders ? (
                <div className="bg-sage-500/10 border border-sage-500/20 rounded-xl p-4 flex items-center gap-3 mb-4">
                  <svg className="w-5 h-5 text-sage-400 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                  </svg>
                  <span className="text-sm text-sage-300">{t('onboarding.providersConfigured', { count: providerCount })}</span>
                </div>
              ) : null}

              {added ? (
                <div className="bg-sage-500/10 border border-sage-500/20 rounded-xl p-4 flex items-center gap-3">
                  <svg className="w-5 h-5 text-sage-400 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                  </svg>
                  <span className="text-sm text-sage-300">{t('onboarding.providerAdded')}</span>
                </div>
              ) : (
                <div className="bg-surface-overlay rounded-xl p-4 border border-surface-border space-y-3">
                  <div>
                    <label className="text-xs text-white/40 mb-1 block">{t('onboarding.providerType')}</label>
                    <select
                      value={providerType}
                      onChange={e => { setProviderType(e.target.value); setBaseUrl(''); setApiKey(''); setModels(''); setAdded(false); setProviderError(''); }}
                      className="w-full bg-black/20 border border-surface-border rounded-lg px-3 py-2 text-sm text-white/80"
                    >
                      <option value="openai">{t('onboarding.providerTypeOpenAI')}</option>
                      <option value="anthropic">{t('onboarding.providerTypeAnthropic')}</option>
                      <option value="ollama">{t('onboarding.providerTypeOllama')}</option>
                    </select>
                    <p className="text-[10px] text-white/20 mt-1">{providerDefaults[providerType].label}</p>
                  </div>
                  <div>
                    <label className="text-xs text-white/40 mb-1 block">
                      {t('onboarding.baseUrl')}
                      <span className="text-white/20 ml-1">{t('onboarding.required')}</span>
                    </label>
                    <input
                      type="text"
                      value={baseUrl}
                      onChange={e => setBaseUrl(e.target.value)}
                      placeholder={providerDefaults[providerType].baseUrl}
                      className="w-full bg-black/20 border border-surface-border rounded-lg px-3 py-2 text-sm text-white/80 placeholder:text-white/20"
                    />
                  </div>
                  <div>
                    <label className="text-xs text-white/40 mb-1 block">
                      {t('onboarding.apiKey')}
                      {providerDefaults[providerType].needKey
                        ? <span className="text-white/20 ml-1">{t('onboarding.required')}</span>
                        : <span className="text-white/20 ml-1">— {t('onboarding.notNeeded')}</span>
                      }
                    </label>
                    <input
                      type="password"
                      value={apiKey}
                      onChange={e => setApiKey(e.target.value)}
                      placeholder="sk-..."
                      className="w-full bg-black/20 border border-surface-border rounded-lg px-3 py-2 text-sm text-white/80 placeholder:text-white/20"
                    />
                  </div>
                  <div>
                    <label className="text-xs text-white/40 mb-1 block">
                      {t('onboarding.models')}
                      <span className="text-white/20 ml-1">{t('onboarding.commaSeparated')}</span>
                    </label>
                    <input
                      type="text"
                      value={models}
                      onChange={e => setModels(e.target.value)}
                      placeholder={providerDefaults[providerType].defaultModels}
                      className="w-full bg-black/20 border border-surface-border rounded-lg px-3 py-2 text-sm text-white/80 placeholder:text-white/20"
                    />
                  </div>
                  {providerError && (
                    <p className="text-xs text-red-400">{providerError}</p>
                  )}
                  <button
                    onClick={addProvider}
                    disabled={adding}
                    className="w-full py-2 bg-ocean-500/20 text-ocean-400 text-sm rounded-lg hover:bg-ocean-500/30 transition-colors disabled:opacity-50"
                  >
                    {adding ? t('onboarding.adding') : t('onboarding.addProviderBtn')}
                  </button>
                </div>
              )}
            </div>
          )}

          {step === 'workspace' && (
            <div className="space-y-4">
              <h2 className="text-xl font-bold text-white mb-4">{t('onboarding.workspaceTitle')}</h2>
              <p className="text-white/60 text-sm mb-4">{t('onboarding.workspaceDesc')}</p>
              <div className="bg-surface-overlay rounded-xl p-4 border border-surface-border space-y-3">
                <div>
                  <label className="text-xs text-white/40 mb-1 block">{t('onboarding.workspacePath')}</label>
                  <input
                    type="text"
                    value={workspaceInput}
                    onChange={e => { setWorkspaceInput(e.target.value); setWorkspaceChanged(e.target.value !== workspace); }}
                    placeholder="&lt;exe_dir&gt;/data"
                    className="w-full bg-black/20 border border-surface-border rounded-lg px-3 py-2 text-sm text-white/80 font-mono placeholder:text-white/20"
                  />
                </div>
                {workspaceChanged && (
                  <div className="space-y-2">
                    {workspaceMsg && (
                      <div className={`text-xs ${workspaceMsg.includes('failed') ? 'text-red-400' : 'text-sage-400'}`}>
                        {workspaceMsg}
                      </div>
                    )}
                    <div className="flex items-center gap-2">
                      <button
                        disabled={applying}
                        onClick={async () => {
                          setApplying(true);
                          setWorkspaceMsg('');
                          try {
                            await api.setWorkspace(workspaceInput);
                            setWorkspace(workspaceInput);
                            setWorkspaceChanged(false);
                            setWorkspaceMsg(t('onboarding.workspaceUpdated'));
                          } catch (e: any) {
                            setWorkspaceChanged(true);
                            setWorkspaceMsg(t('onboarding.applyFailed', { error: e?.message || String(e) }));
                          } finally {
                            setApplying(false);
                          }
                        }}
                        className="px-3 py-1.5 bg-ocean-500/20 text-ocean-400 text-xs rounded-lg hover:bg-ocean-500/30 transition-colors disabled:opacity-50"
                      >
                        {applying ? t('onboarding.applying') : t('onboarding.apply')}
                      </button>
                      <span className="text-[10px] text-amber-400/60">{t('onboarding.restartNote')}</span>
                    </div>
                  </div>
                )}
              </div>
              <p className="text-xs text-white/20">{t('onboarding.workspaceNote')} {t('onboarding.workspaceChangeLater')}</p>
            </div>
          )}

          {step === 'skills' && (
            <div className="space-y-4">
              <h2 className="text-xl font-bold text-white mb-4">{t('onboarding.skillsTitle')}</h2>
              <p className="text-white/60 text-sm mb-6">{t('onboarding.skillsDesc')}</p>
              <div className="grid grid-cols-2 gap-3">
                {[
                  { icon: 'M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z', label: t('memory.title'), desc: t('onboarding.skillsMemory') },
                  { icon: 'M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.066 2.573c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.573 1.066c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.066-2.573c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z', label: t('nav.tools'), desc: t('onboarding.skillsTools') },
                  { icon: 'M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z', label: t('nav.notifications'), desc: t('onboarding.skillsApprovals') },
                  { icon: 'M4 5a1 1 0 011-1h14a1 1 0 011 1v2a1 1 0 01-1 1H5a1 1 0 01-1-1V5zm0 8a1 1 0 011-1h6a1 1 0 011 1v6a1 1 0 01-1 1H5a1 1 0 01-1-1v-6zm12 0a1 1 0 011-1h2a1 1 0 011 1v6a1 1 0 01-1 1h-2a1 1 0 01-1-1v-6z', label: t('nav.dashboard'), desc: t('onboarding.skillsDashboard') },
                ].map(item => (
                  <div key={item.label} className="bg-surface-overlay rounded-xl p-3 border border-surface-border text-center">
                    <svg className="w-5 h-5 mx-auto mb-2 text-ocean-400/60" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d={item.icon} />
                    </svg>
                    <div className="text-xs text-white/70 font-medium">{item.label}</div>
                    <div className="text-[10px] text-white/30 mt-0.5">{item.desc}</div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {step === 'voice' && (
            <div className="space-y-4">
              <h2 className="text-xl font-bold text-white mb-4">{t('onboarding.voiceTitle')}</h2>
              <p className="text-white/60 text-sm mb-6">{t('onboarding.voiceDesc')}</p>
              <div className="bg-surface-overlay rounded-xl p-4 border border-surface-border space-y-3">
                <div className="flex items-center gap-3">
                  <div className="w-8 h-8 rounded-lg bg-ocean-500/15 flex items-center justify-center">
                    <svg className="w-4 h-4 text-ocean-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 11a7 7 0 01-7 7m0 0a7 7 0 01-7-7m7 7v4m0 0H8m4 0h4m-4-8a3 3 0 01-3-3V5a3 3 0 116 0v6a3 3 0 01-3 3z" />
                    </svg>
                  </div>
                  <div>
                    <div className="text-sm text-white/70">{t('onboarding.voiceStt')}</div>
                    <div className="text-xs text-white/30">{t('onboarding.voiceSttDesc')}</div>
                  </div>
                </div>
                <div className="flex items-center gap-3">
                  <div className="w-8 h-8 rounded-lg bg-sage-500/15 flex items-center justify-center">
                    <svg className="w-4 h-4 text-sage-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15.536 8.464a5 5 0 010 7.072m2.828-9.9a9 9 0 010 12.728M5.586 15H4a1 1 0 01-1-1v-4a1 1 0 011-1h1.586l4.707-4.707C10.923 3.663 12 4.109 12 5v14c0 .891-1.077 1.337-1.707.707L5.586 15z" />
                    </svg>
                  </div>
                  <div>
                    <div className="text-sm text-white/70">{t('onboarding.voiceTts')}</div>
                    <div className="text-xs text-white/30">{t('onboarding.voiceTtsDesc')}</div>
                  </div>
                </div>
              </div>
              <p className="text-xs text-white/20 mt-2">{t('onboarding.voiceNote')}</p>
            </div>
          )}

          {step === 'ready' && (
            <div className="text-center space-y-6">
              <div className="w-16 h-16 mx-auto rounded-full bg-sage-500/20 flex items-center justify-center">
                <svg className="w-8 h-8 text-sage-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                </svg>
              </div>
              <div>
                <h2 className="text-2xl font-bold text-white mb-2">{t('onboarding.readyTitle')}</h2>
                <p className="text-white/60 text-sm leading-relaxed">{t('onboarding.readyDesc')}</p>
                {!hasProviders && (
                  <p className="text-amber-400/70 text-xs mt-4">{t('onboarding.readyReminder')}</p>
                )}
              </div>
            </div>
          )}
        </div>

        {/* Actions */}
        <div className="flex items-center justify-between mt-8 pt-6 border-t border-surface-border">
          <button
            onClick={dismiss}
            className="text-white/30 hover:text-white/60 text-sm transition-colors"
          >
            {t('onboarding.skip')}
          </button>
          <div className="flex gap-3">
            {currentIdx > 0 && (
              <button
                onClick={() => setStep(steps[currentIdx - 1].key)}
                className="px-4 py-2 text-sm text-white/60 hover:text-white rounded-lg transition-colors"
              >
                {t('onboarding.back')}
              </button>
            )}
            {step !== 'ready' ? (
              <button
                onClick={() => setStep(steps[currentIdx + 1].key)}
                className="px-5 py-2 bg-ocean-500 text-white text-sm font-medium rounded-lg hover:bg-ocean-600 transition-colors"
              >
                {t('onboarding.next')}
              </button>
            ) : (
              <button
                onClick={finish}
                className="px-5 py-2 bg-ocean-500 text-white text-sm font-medium rounded-lg hover:bg-ocean-600 transition-colors"
              >
                {t('onboarding.getStarted')}
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
