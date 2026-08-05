import { useState, useEffect } from 'react';
import * as api from '../../services/wails';
import type { AllowlistEntry } from '../../services/wails';
import { useApp } from '../../state/AppContext';
import { useT } from '../../lib/i18n/I18nContext';
import { getTheme, setTheme } from '../ThemeSwitcher';
import { ConfigSettings } from './ConfigSettings';
import { PromptSettings } from './PromptSettings';
import { ProviderSettings } from './ProviderSettings';
import { VoiceSettings } from './VoiceSettings';
import { WebhookSettings } from './WebhookSettings';
import { DiagnosticsSettings } from './DiagnosticsSettings';
import { SoulSettings } from './SoulSettings';
import { ChannelSettings } from './ChannelSettings';
import { ExtensionSettings } from './ExtensionSettings';
import { CompanionSettings } from './CompanionSettings';

type Tab = 'general' | 'providers' | 'approvals' | 'theme' | 'config' | 'prompts' | 'about' | 'autonomy' | 'keyring' | 'webhooks' | 'diagnostics' | 'voice' | 'soul' | 'channels' | 'extensions'  | 'companion';
type Theme = 'dark' | 'light' | 'system';

function HealthCard({ label, value }: { label: string; value: unknown }) {
  const renderValue = (v: unknown): string => {
    if (Array.isArray(v)) {
      if (v.length === 0) return '[]';
      if (v.every(x => typeof x === 'string')) {
        return v.join(', ');
      }
      return JSON.stringify(v);
    }
    if (typeof v === 'object' && v !== null) {
      return JSON.stringify(v);
    }
    return String(v);
  };

  const text = renderValue(value);
  const isLong = text.length > 80;

  return (
    <div className="card min-w-0">
      <div className="text-xs text-white/30 uppercase tracking-wider truncate">{label}</div>
      <div className={`text-sm text-white/70 mt-1 font-mono ${isLong ? 'max-h-32 overflow-y-auto' : ''}`} style={isLong ? { wordBreak: 'break-word', whiteSpace: 'pre-wrap' } : undefined}>
        {text}
      </div>
    </div>
  );
}

function formatCount(val: unknown): string {
  if (val == null) return '—';
  if (Array.isArray(val)) return String(val.length);
  return String(val);
}

export function SettingsPage() {
  const { dispatch } = useApp();
  const { t, locale, setLocale, localeLabels: labels } = useT();
  const [tab, setTab] = useState<Tab>('general');
  const [health, setHealth] = useState<Record<string, unknown>>({});
  const [allowlist, setAllowlist] = useState<AllowlistEntry[]>([]);
  const [cronJobs, setCronJobs] = useState<unknown[]>([]);
  const [showAddCron, setShowAddCron] = useState(false);
  const [cronName, setCronName] = useState('');
  const [cronSched, setCronSched] = useState('');
  const [cronPrompt, setCronPrompt] = useState('');
  const [version, setVersion] = useState('');
  const [currentTheme, setCurrentTheme] = useState<Theme>(getTheme());

  useEffect(() => {
    api.health().then(setHealth).catch(() => {});
    api.listApprovalAllowlist().then(setAllowlist).catch(() => {});
    api.getCronJobs().then(setCronJobs).catch(() => {});
    api.getCurrentVersion().then(setVersion).catch(() => {});
    // Composio requires the composio extension to be loaded at runtime.
  }, [tab]);

  // Re-sync theme state whenever the theme tab is opened.
  useEffect(() => {
    if (tab === 'theme') setCurrentTheme(getTheme());
  }, [tab]);

  const tabs: { id: Tab; key: string }[] = [
    { id: 'general', key: 'settings.general' },
    { id: 'providers', key: 'settings.providers' },
    { id: 'config', key: 'settings.config' },
    { id: 'prompts', key: 'settings.prompts' },
    { id: 'autonomy', key: 'autonomy.title' },
    { id: 'approvals', key: 'settings.approvals' },
    { id: 'voice', key: 'voice.title' },
    { id: 'theme', key: 'settings.theme' },
    { id: 'keyring', key: 'keyring.title' },
    { id: 'webhooks', key: 'webhooks.title' },
    { id: 'diagnostics', key: 'diagnostics.title' },
    { id: 'soul', key: 'settings.soul' },
    { id: 'channels', key: 'settings.channels' },
    { id: 'extensions', key: 'settings.extensions' },
    { id: 'companion', key: 'settings.companion' },
    { id: 'about', key: 'settings.about' },
  ];

  const themeOptions: { id: Theme; labelKey: string; descKey: string; icon: string }[] = [
    { id: 'dark', labelKey: 'settings.darkLabel', descKey: 'settings.darkDesc', icon: '\u{1F319}' },
    { id: 'light', labelKey: 'settings.lightLabel', descKey: 'settings.lightDesc', icon: '\u{2600}\u{FE0F}' },
    { id: 'system', labelKey: 'settings.systemLabel', descKey: 'settings.systemDesc', icon: '\u{1F5A5}\u{FE0F}' },
  ];

  return (
    <div className="flex-1 flex flex-col min-h-0">
      <header className="flex items-center gap-4 px-6 py-3 border-b border-surface-border glass-surface shrink-0">
        <button className="btn-ghost !p-1.5" onClick={() => dispatch({ type: 'TOGGLE_SIDEBAR' })}>
          <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
          </svg>
        </button>
        <h1 className="text-sm font-semibold text-white/90">{t('settings.title')}</h1>
      </header>
      <div className="flex-1 flex overflow-hidden">
        {/* Tab sidebar */}
        <nav className="w-48 shrink-0 border-r border-surface-border p-3 overflow-y-auto space-y-0.5">
          {tabs.map(tb => (
            <button
              key={tb.id}
              onClick={() => setTab(tb.id)}
              className={`w-full text-left px-3 py-2 rounded-lg text-sm transition-all ${
                tab === tb.id ? 'bg-ocean-500/15 text-ocean-300' : 'text-white/50 hover:text-white/80 hover:bg-white/5'
              }`}
            >
              {t(tb.key)}
            </button>
          ))}
        </nav>
        {/* Content */}
        <div className="flex-1 overflow-y-auto p-6">
          {tab === 'general' && (
            <div className="max-w-3xl xl:max-w-5xl 2xl:max-w-6xl space-y-6 animate-fade-in">
              <div className="card">
                <div className="flex items-center justify-between">
                  <div>
                    <div className="text-sm text-white/70">{t('settings.language')}</div>
                    <div className="text-xs text-white/30 mt-0.5">{t('settings.languageDesc')}</div>
                  </div>
                  <select
                    value={locale}
                    onChange={e => setLocale(e.target.value as typeof locale)}
                    className="input-field !w-44 !py-1.5 !text-xs"
                  >
                    {Object.entries(labels).map(([code, label]) => (
                      <option key={code} value={code}>{label}</option>
                    ))}
                  </select>
                </div>
              </div>

              <h2 className="text-lg font-semibold text-white/80">{t('settings.systemHealth')}</h2>
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5 gap-3">
                {Object.entries(health).map(([key, value]) => (
                  <HealthCard key={key} label={key} value={value} />
                ))}
              </div>
              <>
                  <div className="flex items-center justify-between mt-6">
                    <h3 className="text-sm font-semibold text-white/60">{t('settings.cronJobs')}</h3>
                    <button
                      className="text-xs text-ocean-400 hover:text-ocean-300"
                      onClick={() => setShowAddCron(!showAddCron)}
                    >{showAddCron ? 'Cancel' : '+ Add Job'}</button>
                  </div>
                  {showAddCron && (
                    <div className="card space-y-2">
                      <input className="input-field !text-xs" placeholder="Job name" value={cronName} onChange={e => setCronName(e.target.value)} />
                      <input className="input-field !text-xs" placeholder="Schedule (e.g. hourly, daily, 0 9 * * *)" value={cronSched} onChange={e => setCronSched(e.target.value)} />
                      <input className="input-field !text-xs" placeholder="Agent prompt (optional)" value={cronPrompt} onChange={e => setCronPrompt(e.target.value)} />
                      <button
                        className="btn-primary text-xs"
                        onClick={async () => {
                          try {
                            await api.addCronJob(cronName, cronSched, cronPrompt);
                            setCronName(''); setCronSched(''); setCronPrompt(''); setShowAddCron(false);
                            const jobs = await api.getCronJobs();
                            setCronJobs(jobs as unknown[]);
                          } catch (_) {}
                        }}
                        disabled={!cronName || !cronSched}
                      >Save</button>
                    </div>
                  )}
                  {cronJobs.length === 0 ? (
                    <p className="text-xs text-white/30 py-2">No scheduled jobs. Add one above or they are created automatically at startup.</p>
                  ) : (
                  <div className="space-y-2">
                    {cronJobs.map((job: any, i: number) => (
                      <div key={i} className="card flex items-center justify-between">
                        <div>
                          <div className="text-sm text-white/70">{job.name || job.id}</div>
                          <div className="text-xs text-white/30">{job.schedule}</div>
                        </div>
                        <div className="flex items-center gap-2">
                          <button
                            onClick={async () => {
                              try {
                                await api.toggleCronJob(job.id, !job.enabled);
                                const jobs = await api.getCronJobs();
                                setCronJobs(jobs as unknown[]);
                              } catch (_) {}
                            }}
                            className={`text-xs px-2 py-0.5 rounded transition-colors ${
                              job.enabled ? 'bg-coral-500/20 text-coral-400 hover:bg-coral-500/30' : 'bg-sage-500/20 text-sage-400 hover:bg-sage-500/30'
                            }`}
                          >
                            {job.enabled ? 'Disable' : 'Enable'}
                          </button>
                          <button
                            onClick={async () => {
                              try { await api.triggerCronJob(job.id); } catch (_) {}
                            }}
                            className="text-xs px-2 py-0.5 rounded bg-ocean-500/15 text-ocean-400 hover:bg-ocean-500/30 transition-colors"
                          >
                            Run Now
                          </button>
                          <button
                            onClick={async () => {
                              try {
                                await api.removeCronJob(job.id);
                                const jobs = await api.getCronJobs();
                                setCronJobs(jobs as unknown[]);
                              } catch (_) {}
                            }}
                            className="text-xs px-2 py-0.5 rounded text-white/20 hover:text-coral-400 hover:bg-coral-500/10 transition-colors"
                            title="Remove"
                          >x</button>
                        </div>
                      </div>
                    ))}
                  </div>
                  )}
                </>
            </div>
          )}

          {tab === 'providers' && <ProviderSettings />}

          {tab === 'approvals' && (
            <div className="max-w-2xl xl:max-w-3xl 2xl:max-w-4xl animate-fade-in">
              <h2 className="text-lg font-semibold text-white/80 mb-4">{t('settings.approvals')}</h2>
              {allowlist.length === 0 ? (
                <p className="text-sm text-white/30">{t('settings.noAllowlist')}</p>
              ) : (
                <div className="space-y-2">
                  {allowlist.map(entry => (
                    <div key={entry.tool_name} className="card flex items-center justify-between">
                      <div className="text-sm text-white/70 font-mono">{entry.tool_name}</div>
                      <span className="badge bg-sage-500/20 text-sage-400">{t('approval.alwaysAllow')}</span>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}

          {tab === 'theme' && (
            <div className="max-w-2xl xl:max-w-3xl 2xl:max-w-4xl animate-fade-in">
              <h2 className="text-lg font-semibold text-white/80 mb-4">{t('settings.appearance')}</h2>
              <div className="space-y-2">
                {themeOptions.map(opt => (
                  <button
                    key={opt.id}
                    onClick={() => { setTheme(opt.id); setCurrentTheme(opt.id); }}
                    className={`w-full card flex items-center gap-3 text-left transition-all ${
                      currentTheme === opt.id ? 'ring-2 ring-ocean-500' : ''
                    }`}
                  >
                    <span className="text-xl">{opt.icon}</span>
                    <div>
                      <div className="text-sm text-white/80">{t(opt.labelKey)}</div>
                      <div className="text-xs text-white/30">{t(opt.descKey)}</div>
                    </div>
                    {currentTheme === opt.id && <div className="ml-auto w-2.5 h-2.5 rounded-full bg-ocean-400" />}
                  </button>
                ))}
              </div>
            </div>
          )}

          {tab === 'config' && <ConfigSettings />}

          {tab === 'prompts' && <PromptSettings />}

          {tab === 'about' && (
            <div className="max-w-2xl xl:max-w-3xl 2xl:max-w-4xl animate-fade-in space-y-4">
              <h2 className="text-lg font-semibold text-white/80">{t('settings.about')}</h2>
              <div className="card space-y-2">
                <div className="flex justify-between text-sm">
                  <span className="text-white/40">{t('settings.version')}</span>
                  <span className="text-white/70 font-mono">{version || 'dev'}</span>
                </div>
                <div className="flex justify-between text-sm">
                  <span className="text-white/40">{t('settings.toolsRegistered')}</span>
                  <span className="text-white/70 font-mono">{formatCount(health?.tools)}</span>
                </div>
                <div className="flex justify-between text-sm">
                  <span className="text-white/40">{t('settings.agentsAvailable')}</span>
                  <span className="text-white/70 font-mono">{formatCount(health?.agents)}</span>
                </div>
                <div className="flex justify-between text-sm gap-3">
                  <span className="text-white/40 shrink-0">{t('settings.dataDir')}</span>
                  <span className="text-white/70 font-mono text-xs truncate max-w-[280px]" title={health?.workspace ? String(health.workspace) : undefined}>{health?.workspace ? String(health.workspace) : '—'}</span>
                </div>
              </div>
              <button className="btn-primary text-sm" onClick={() => api.checkForUpdate().then(r => alert(JSON.stringify(r)))}>
                {t('settings.checkUpdates')}
              </button>
            </div>
          )}

          {tab === 'autonomy' && (
            <div className="max-w-2xl xl:max-w-3xl 2xl:max-w-4xl animate-fade-in space-y-4">
              <h2 className="text-lg font-semibold">{t('autonomy.title')}</h2>
              <div className="card space-y-3 text-sm">
                <p className="text-secondary">{t('autonomy.level')}: <strong>{String(health?.tier || 'supervised')}</strong></p>
                <p className="text-secondary">{t('autonomy.workspaceOnly')}: <strong>{health?.workspace_only ? 'Yes' : 'No'}</strong></p>
              </div>
            </div>
          )}

          {tab === 'keyring' && (
            <div className="max-w-2xl xl:max-w-3xl 2xl:max-w-4xl animate-fade-in space-y-4">
              <h2 className="text-lg font-semibold">{t('keyring.title')}</h2>
              <div className="card space-y-3 text-sm">
                <div className="flex justify-between"><span>{t('keyring.status')}</span><span className="font-mono">{String((health?.keyring_status as any)?.available ? t('keyring.available') : t('keyring.unavailable'))}</span></div>
                <div className="flex justify-between"><span>{t('keyring.mode')}</span><span className="font-mono">{String((health?.keyring_status as any)?.activeMode || '—')}</span></div>
                <div className="flex justify-between"><span>{t('keyring.backend')}</span><span className="font-mono">{String((health?.keyring_status as any)?.backendName || '—')}</span></div>
              </div>
              <button className="btn-primary text-sm" onClick={async () => {
                await api.keyringRetryProbe();
                const h = await api.health();
                setHealth(h);
              }}>
                {t('keyring.retryProbe')}
              </button>
            </div>
          )}

          {tab === 'webhooks' && <WebhookSettings />}

          {tab === 'diagnostics' && <DiagnosticsSettings />}

          {tab === 'voice' && <VoiceSettings />}

        {tab === 'soul' && <SoulSettings />}
        {tab === 'channels' && <ChannelSettings />}
        {tab === 'companion' && <CompanionSettings />}
        {tab === 'extensions' && <ExtensionSettings />}
        </div>
      </div>
    </div>
  );
}
