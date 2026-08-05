import { useState, useEffect, useCallback } from 'react';
import * as api from '../../services/wails';
import { useT } from '../../lib/i18n/I18nContext';
import type { PromptMeta } from '../../services/wails';

/** Resolve a human-readable label for a prompt name via i18n. */
function promptLabel(t: (key: string) => string, name: string): string {
  const key = `settings.promptLabels.${name}`;
  const translated = t(key);
  if (translated === key) {
    return name
      .replace(/_/g, ' ')
      .replace(/\b\w/g, c => c.toUpperCase());
  }
  return translated;
}

export function PromptSettings() {
  const { t } = useT();
  const [prompts, setPrompts] = useState<PromptMeta[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [body, setBody] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [msg, setMsg] = useState('');
  const [showDefault, setShowDefault] = useState(false);
  const [defaultText, setDefaultText] = useState('');
  const [defaultLoading, setDefaultLoading] = useState(false);
  const [adding, setAdding] = useState(false);
  const [newName, setNewName] = useState('');

  const loadPrompts = useCallback(async () => {
    try {
      const list = await api.listPrompts();
      setPrompts(list);
    } catch { /* ignore */ }
    finally { setLoading(false); }
  }, []);

  useEffect(() => { loadPrompts(); }, [loadPrompts]);

  const select = async (name: string) => {
    setSelected(name);
    setMsg('');
    setShowDefault(false);
    setDefaultText('');
    setDefaultLoading(false);
    try {
      const text = await api.getPrompt(name);
      setBody(text);
    } catch (e) {
      setBody(`${t('errors.somethingWrong')}: ${e}`);
    }
  };

  const toggleDefault = async () => {
    if (!showDefault && selected && !defaultText) {
      setDefaultLoading(true);
      try {
        const text = await api.getDefaultPrompt(selected);
        if (text) {
          setDefaultText(text);
        } else {
          // Go method returned empty string — embed might have failed.
          console.warn(`GetDefaultPrompt returned empty string for "${selected}"`);
          setDefaultText('');
        }
      } catch (e) {
        console.error(`GetDefaultPrompt failed for "${selected}":`, e);
        setDefaultText('');
      } finally {
        setDefaultLoading(false);
      }
    }
    setShowDefault(!showDefault);
  };

  const handleSave = async () => {
    if (!selected) return;
    setSaving(true);
    try {
      await api.setPrompt(selected, body);
      setMsg(t('settings.save'));
      await loadPrompts();
    } catch (e) {
      setMsg(`${t('errors.somethingWrong')}: ${e}`);
    } finally {
      setSaving(false);
      setTimeout(() => setMsg(''), 3000);
    }
  };

  const reset = async () => {
    if (!selected) return;
    setSaving(true);
    try {
      await api.setPrompt(selected, '');
      setMsg(t('settings.resetToDefault'));
      await select(selected);
      await loadPrompts();
    } catch (e) {
      setMsg(`${t('errors.somethingWrong')}: ${e}`);
    } finally {
      setSaving(false);
      setTimeout(() => setMsg(''), 3000);
    }
  };

  const handleDelete = async () => {
    if (!selected) return;
    const p = prompts.find(x => x.name === selected);
    if (p?.builtin) return;
    setSaving(true);
    try {
      await api.deletePrompt(selected);
      setSelected(null);
      setBody('');
      setMsg(t('settings.save'));
      await loadPrompts();
    } catch (e) {
      setMsg(`${t('errors.somethingWrong')}: ${e}`);
    } finally {
      setSaving(false);
      setTimeout(() => setMsg(''), 3000);
    }
  };

  const handleCreate = async () => {
    const name = newName.trim().toLowerCase().replace(/\s+/g, '_').replace(/[^a-z0-9_]/g, '');
    if (!name) return;
    setSaving(true);
    try {
      await api.setPrompt(name, '');
      setNewName('');
      setAdding(false);
      await loadPrompts();
      select(name);
    } catch (e) {
      setMsg(`${t('errors.somethingWrong')}: ${e}`);
    } finally {
      setSaving(false);
      setTimeout(() => setMsg(''), 3000);
    }
  };

  if (loading) {
    return <div className="animate-pulse space-y-2">{[1, 2, 3, 4].map(i => <div key={i} className="h-8 bg-surface-muted rounded" />)}</div>;
  }

  const selectedMeta = prompts.find(x => x.name === selected);

  return (
    <div className="animate-fade-in h-full">
      <div className="flex gap-6 h-full">
        {/* Prompt list */}
        <div className="w-56 shrink-0 space-y-0.5">
          <div className="flex items-center justify-between mb-3 px-1">
            <h3 className="text-xs font-semibold text-white/40 uppercase tracking-wider">
              {t('settings.promptTemplates')}
            </h3>
            <button className="btn-ghost !text-xs !py-1 !px-2" onClick={() => setAdding(true)}>
              + New
            </button>
          </div>

          {adding && (
            <div className="px-1 pb-2 space-y-1.5">
              <input
                value={newName}
                onChange={e => setNewName(e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter') handleCreate(); if (e.key === 'Escape') setAdding(false); }}
                placeholder="my_prompt"
                className="input-field !py-1 !text-xs w-full"
                autoFocus
              />
              <div className="flex gap-1">
                <button className="btn-primary !text-[10px] !py-0.5 !px-2" onClick={handleCreate} disabled={saving}>
                  {t('settings.save')}
                </button>
                <button className="btn-ghost !text-[10px] !py-0.5 !px-2" onClick={() => setAdding(false)}>
                  {t('settings.cancel')}
                </button>
              </div>
            </div>
          )}

          {prompts.map(p => (
            <button key={p.name} onClick={() => select(p.name)}
              className={`w-full text-left px-2.5 py-2 rounded-md text-xs transition-all ${
                selected === p.name ? 'bg-ocean-500/15 text-ocean-300' : 'text-white/50 hover:text-white/80 hover:bg-white/5'
              }`}>
              <div className="flex items-center gap-1.5">
                <span className="truncate flex-1 font-medium">{promptLabel(t, p.name)}</span>
                {!p.builtin && (
                  <span className="text-[9px] px-1 rounded bg-amber-500/15 text-amber-400 shrink-0">custom</span>
                )}
                {p.overridden && (
                  <span className="w-1.5 h-1.5 rounded-full bg-amber-400 shrink-0" title={t('settings.customizedHint')} />
                )}
              </div>
              {p.description && (
                <p className="text-[10px] text-white/25 mt-0.5 line-clamp-2 leading-relaxed">{p.description}</p>
              )}
            </button>
          ))}
        </div>

        {/* Editor */}
        <div className="flex-1 min-w-0">
          {selected ? (
            <div className="card">
              <div className="flex items-center justify-between mb-3 gap-2 flex-wrap">
                <div className="min-w-[200px] flex-1">
                  <h3 className="text-sm font-semibold text-white/80 truncate">{promptLabel(t, selected)}</h3>
                  <p className="text-xs text-white/25 font-mono mt-0.5 truncate">{selected}</p>
                  {selectedMeta?.description && (
                    <p className="text-xs text-white/30 mt-1 leading-relaxed">{selectedMeta.description}</p>
                  )}
                </div>
                {selectedMeta?.builtin && (
                  <button className="btn-ghost !text-xs" onClick={toggleDefault}>
                    {showDefault ? t('settings.hideDefault') || 'Hide default' : t('settings.viewDefault') || 'View default'}
                  </button>
                )}
                {selectedMeta?.builtin ? (
                  <button className="btn-ghost !text-xs" onClick={reset} disabled={saving}>
                    {t('settings.reset')}
                  </button>
                ) : (
                  <button className="btn-ghost !text-xs !text-coral-400" onClick={handleDelete} disabled={saving}>
                    {t('settings.delete') || 'Delete'}
                  </button>
                )}
                <button className="btn-primary text-xs" onClick={handleSave} disabled={saving}>
                  {saving ? t('settings.saving') : t('settings.save')}
                </button>
              </div>
              {msg && (
                <div className={`mb-3 text-xs ${msg.includes(t('errors.somethingWrong')) ? 'text-coral-400' : 'text-sage-400'} animate-fade-in`}>
                  {msg}
                </div>
              )}
              <textarea value={body} onChange={e => setBody(e.target.value)}
                className="input-field !font-mono !text-xs w-full min-h-[260px] resize-y"
                placeholder="Prompt template text..." />
              {showDefault && (
                <div className="mt-3 p-3 rounded-md bg-surface-muted border border-surface-border">
                  <div className="text-[10px] text-white/30 uppercase tracking-wider mb-1">
                    {t('settings.originalDefault') || 'Original default'}
                  </div>
                  {defaultLoading ? (
                    <p className="text-xs text-white/20 italic">Loading...</p>
                  ) : defaultText ? (
                    <pre className="text-xs text-white/40 whitespace-pre-wrap font-mono leading-relaxed">{defaultText}</pre>
                  ) : (
                    <p className="text-xs text-white/20 italic">Not available — rebuild the Go binary if this persists.</p>
                  )}
                </div>
              )}
              <div className="flex items-center justify-between mt-2 text-xs text-white/25">
                <span>{t('settings.chars', { count: body.length })}</span>
                <span>{t('settings.charsDefault', { count: selectedMeta?.default_length || 0 })}</span>
              </div>
            </div>
          ) : (
            <div className="card text-center py-16">
              <p className="text-sm text-white/25">{t('settings.selectPrompt')}</p>
              <p className="text-xs text-white/10 mt-1">{t('settings.customizedHint')}</p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
