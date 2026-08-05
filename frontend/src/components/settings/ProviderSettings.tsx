import { useState, useEffect, useCallback } from 'react';
import * as api from '../../services/wails';
import { useT } from '../../lib/i18n/I18nContext';
import type { ProviderConfig } from '../../services/wails';

const PROVIDER_TYPES = ['openai', 'anthropic', 'ollama'];

function maskKey(key: string): string {
  if (!key || key.includes('****')) return key || '—';
  if (key.length <= 8) return '****';
  return key.slice(0, 4) + '****' + key.slice(-4);
}

export function ProviderSettings() {
  const { t } = useT();
  const [providers, setProviders] = useState<ProviderConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState<ProviderConfig | null>(null);
  const [adding, setAdding] = useState(false);
  const [defaultModel, setDefaultModel] = useState('');
  const [form, setForm] = useState<ProviderConfig>({ name: '', type: 'openai', api_key: '', base_url: '', models: [] });
  const [modelInput, setModelInput] = useState('');
  const [msg, setMsg] = useState('');
  const [saving, setSaving] = useState(false);
  const [modelRoutes, setModelRoutes] = useState<Record<string, string>>({});

  const ROUTE_KINDS: { kind: string; labelKey: string }[] = [
    { kind: 'coding', labelKey: 'settings.modelRoutingCoding' },
    { kind: 'reasoning', labelKey: 'settings.modelRoutingReasoning' },
    { kind: 'summary', labelKey: 'settings.modelRoutingSummary' },
    { kind: 'vision', labelKey: 'settings.modelRoutingVision' },
  ];

  // Collect all model names from all providers for datalist suggestions.
  const allModels = [...new Set(providers.flatMap(p => p.models || []))];

  const flash = (text: string) => { setMsg(text); setTimeout(() => setMsg(''), 3000); };

  const load = useCallback(async () => {
    try {
      const [p] = await Promise.all([api.listProviders()]);
      setProviders(p || []);
    } catch { /* ignore */ }
    finally { setLoading(false); }
  }, []);

  useEffect(() => { load(); }, [load]);
  useEffect(() => { api.listModels().then(m => {
    if (m?.default_model) setDefaultModel(m.default_model);
  }).catch(() => { api.health().then(h => setDefaultModel(String(h?.model || ''))).catch(() => {}); }); }, []);
  useEffect(() => { api.getModelRoutes().then(r => setModelRoutes(r || {})).catch(() => {}); }, []);

  // All individual models from all providers, for the default model dropdown.
  // Users select an actual API model name, not a provider name.
  const allProviderModels = providers.flatMap(p =>
    (p.models || []).map(m => ({ provider: p.name, model: m }))
  );

  const [testingProvider, setTestingProvider] = useState<string | null>(null);

  const testConnection = async (provider: ProviderConfig) => {
    setTestingProvider(provider.name);
    try {
      const result = await api.testProviderConnection(provider.name);
      if (result.ok) {
        flash(`${t('settings.testConnectionOk')} — ${result.endpoint} (HTTP ${result.status})`);
      } else {
        flash(`${t('settings.testConnectionFailed')}: ${result.error || result.warning || 'Unknown error'}`);
      }
    } catch (e) {
      flash(`${t('settings.testConnectionFailed')}: ${e}`);
    } finally {
      setTestingProvider(null);
    }
  };

  const save = async (fn: () => Promise<void>) => {
    setSaving(true);
    try { await fn(); flash(t('settings.save')); await load(); cancelEdit(); }
    catch (e) { flash(`${t('errors.somethingWrong')}: ${e}`); }
    finally { setSaving(false); }
  };

  const cancelEdit = () => { setEditing(null); setAdding(false); setForm({ name: '', type: 'openai', api_key: '', base_url: '', models: [] }); setModelInput(''); };

  const startEdit = (p: ProviderConfig) => {
    setEditing(p);
    setAdding(false);
    setForm({ ...p, models: [...(p.models || [])] });
  };

  const startAdd = () => {
    setAdding(true);
    setEditing(null);
    setForm({ name: '', type: 'openai', api_key: '', base_url: '', models: [] });
    setModelInput('');
  };

  const addModel = () => {
    const m = modelInput.trim();
    if (m && !form.models.includes(m)) {
      setForm({ ...form, models: [...form.models, m] });
      setModelInput('');
    }
  };

  const removeModel = (m: string) => {
    setForm({ ...form, models: form.models.filter(x => x !== m) });
  };

  if (loading) {
    return <div className="animate-pulse space-y-3">{[1, 2, 3].map(i => <div key={i} className="h-16 bg-surface-muted rounded-lg" />)}</div>;
  }

  return (
    <div className="max-w-2xl xl:max-w-3xl 2xl:max-w-4xl animate-fade-in">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-semibold text-white/80">{t('settings.providers')}</h2>
        <button className="btn-primary text-xs" onClick={startAdd}>{t('settings.addProvider')}</button>
      </div>

      {msg && <div className={`mb-3 text-xs ${msg.includes(t('errors.somethingWrong')) ? 'text-coral-400' : 'text-sage-400'} animate-fade-in`}>{msg}</div>}

      {/* Default provider */}
      <div className="card mb-4">
        <div className="flex items-center justify-between">
          <div>
            <div className="text-sm text-white/70">{t('settings.defaultProvider')}</div>
            <div className="text-xs text-white/25 mt-0.5">{t('settings.defaultProviderDesc')}</div>
          </div>
          <select
            value={defaultModel}
            onChange={e => {
              const v = e.target.value;
              setDefaultModel(v);
              api.setDefaultModel(v).then(() => flash(t('settings.save'))).catch(() => {});
            }}
            className="input-field !w-44 !py-1.5 !text-xs">
            <option value="">—</option>
            {providers.map(p => (
              <optgroup key={p.name} label={`${p.name} (${p.type})`}>
                {(p.models || []).map(m => (
                  <option key={m} value={m}>{m}</option>
                ))}
              </optgroup>
            ))}
          </select>
        </div>
      </div>

      {/* Model routes per task kind */}
      <div className="card mb-4 space-y-3">
        <div className="text-sm text-white/70">{t('settings.modelRouting')}</div>
        {ROUTE_KINDS.map(({ kind, labelKey }) => (
          <div key={kind} className="flex items-center justify-between">
            <span className="text-xs text-white/50 w-20">{t(labelKey)}</span>
            <input
              list={`models-${kind}`}
              value={modelRoutes[kind] || ''}
              onChange={e => {
                const v = e.target.value;
                setModelRoutes(prev => ({ ...prev, [kind]: v }));
              }}
              onBlur={() => {
                api.setModelRoute(kind, modelRoutes[kind] || '').then(() => flash(t('settings.save'))).catch(() => {});
              }}
              placeholder={t('settings.useDefault')}
              className="input-field !py-1.5 !text-xs flex-1 ml-4"
            />
            <datalist id={`models-${kind}`}>
              {allModels.map(m => <option key={m} value={m} />)}
            </datalist>
          </div>
        ))}
      </div>

      {providers.length === 0 && (
        <div className="card text-center py-8">
          <p className="text-sm text-white/25">{t('settings.noProviders')}</p>
          <p className="text-xs text-white/15 mt-1">{t('settings.noProvidersHint')}</p>
        </div>
      )}

      <div className="space-y-2">
        {providers.map(p => (
          <div key={p.name} className="card flex items-center justify-between">
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2 min-w-0">
                <span className="text-sm text-white/80 font-medium truncate">{p.name}</span>
                <span className="badge text-[10px] bg-ocean-500/15 text-ocean-400 shrink-0">{p.type}</span>
              </div>
              <div className="flex items-center gap-3 mt-1 text-xs text-white/30 min-w-0">
                <span className="truncate">{p.base_url || '—'}</span>
                <span className="font-mono shrink-0">{maskKey(p.api_key)}</span>
                <span className="shrink-0">{p.models?.length || 0} {t('settings.models').toLowerCase()}</span>
              </div>
            </div>
            <div className="flex gap-1.5 shrink-0">
              <button className="btn-ghost !text-xs !px-2" onClick={() => startEdit(p)}>{t('settings.edit')}</button>
              <button
                className="btn-ghost !text-xs !text-ocean-400 !px-2"
                disabled={testingProvider === p.name}
                onClick={() => testConnection(p)}
              >
                {testingProvider === p.name ? '...' : t('settings.test')}
              </button>
              <button className="btn-ghost !text-xs !text-coral-400 !px-2" onClick={() => save(() => api.removeProvider(p.name))}>{t('settings.remove')}</button>
            </div>
          </div>
        ))}
      </div>

      {(adding || editing) && (
        <div className="card mt-4 space-y-3">
          <h3 className="text-sm font-semibold text-white/70">
            {editing ? t('settings.editProvider', { name: editing.name }) : t('settings.addNewProvider')}
          </h3>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="text-xs text-white/30 block mb-1">{t('settings.name')}</label>
              <input value={form.name} onChange={e => setForm({ ...form, name: e.target.value })}
                className="input-field !py-1.5 !text-xs" placeholder="e.g. openai" />
            </div>
            <div>
              <label className="text-xs text-white/30 block mb-1">{t('settings.type')}</label>
              <select value={form.type} onChange={e => setForm({ ...form, type: e.target.value })}
                className="input-field !py-1.5 !text-xs">
                {PROVIDER_TYPES.map(ty => <option key={ty} value={ty}>{ty}</option>)}
              </select>
            </div>
          </div>
          <div>
            <label className="text-xs text-white/30 block mb-1">{t('settings.apiKey')}</label>
            <input value={form.api_key} onChange={e => setForm({ ...form, api_key: e.target.value })}
              className="input-field !py-1.5 !text-xs" type="password" placeholder="sk-..." />
          </div>
          <div>
            <label className="text-xs text-white/30 block mb-1">{t('settings.baseUrl')}</label>
            <input value={form.base_url} onChange={e => setForm({ ...form, base_url: e.target.value })}
              className="input-field !py-1.5 !text-xs" placeholder="https://api.openai.com/v1" />
          </div>
          <div>
            <label className="text-xs text-white/30 block mb-1">{t('settings.models')}</label>
            <div className="flex gap-2">
              <input value={modelInput} onChange={e => setModelInput(e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); addModel(); } }}
                className="input-field !py-1.5 !text-xs flex-1" placeholder="e.g. gpt-4o" />
              <button className="btn-ghost !text-xs" onClick={addModel}>{t('settings.addModel')}</button>
            </div>
            {form.models.length > 0 && (
              <div className="flex flex-wrap gap-1.5 mt-2">
                {form.models.map(m => (
                  <span key={m} className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs bg-ocean-500/15 text-ocean-400 max-w-[200px]">
                    <span className="truncate">{m}</span>
                    <button onClick={() => removeModel(m)} className="text-white/30 hover:text-coral-400 shrink-0">&times;</button>
                  </span>
                ))}
              </div>
            )}
          </div>
          <div className="flex gap-2 pt-2">
            <button className="btn-primary text-xs" disabled={saving || !form.name}
              onClick={() => save(() => editing ? api.updateProvider(editing.name, form) : api.addProvider(form))}>
              {saving ? t('settings.saving') : editing ? t('settings.update') : t('settings.save')}
            </button>
            <button className="btn-ghost text-xs" onClick={cancelEdit}>{t('settings.cancel')}</button>
          </div>
        </div>
      )}
    </div>
  );
}
