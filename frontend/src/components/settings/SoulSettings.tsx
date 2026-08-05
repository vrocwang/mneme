import { useState, useEffect, useCallback } from 'react';
import { getSOUL, setSOUL, getIdentity, setIdentity } from '../../services/wails';
import { useT } from '../../lib/i18n/I18nContext';

type SaveState = 'idle' | 'saving' | 'saved' | 'error';

function useEditorState(fetchFn: () => Promise<string>, saveFn: (content: string) => Promise<void>) {
  const [content, setContent] = useState('');
  const [original, setOriginal] = useState('');
  const [state, setState] = useState<SaveState>('idle');
  const [error, setError] = useState('');

  useEffect(() => {
    fetchFn().then((text) => { setContent(text); setOriginal(text); }).catch(() => {});
  }, []);

  const save = useCallback(async () => {
    setState('saving');
    try {
      await saveFn(content);
      setOriginal(content);
      setState('saved');
      setTimeout(() => setState('idle'), 2000);
    } catch (e) {
      setError(String(e));
      setState('error');
    }
  }, [content, saveFn]);

  const dirty = content !== original;
  return { content, setContent, dirty, state, error, save };
}

export function SoulSettings() {
  const { t } = useT();
  const soul = useEditorState(getSOUL, setSOUL);
  const identity = useEditorState(getIdentity, setIdentity);

  const saveLabel = (s: SaveState) =>
    s === 'saving' ? t('soul.saving') :
    s === 'saved' ? t('soul.saved') :
    s === 'error' ? t('soul.error') : t('soul.save');

  return (
    <div className="max-w-2xl xl:max-w-3xl 2xl:max-w-4xl animate-fade-in space-y-6">
      {/* SOUL.md */}
      <section>
        <div className="flex items-center justify-between mb-2">
          <div>
            <h3 className="text-sm font-semibold text-white/80">{t('soul.soulTitle')}</h3>
            <p className="text-xs text-white/30 mt-0.5">{t('soul.soulDesc')}</p>
          </div>
          <button
            onClick={soul.save}
            disabled={!soul.dirty || soul.state === 'saving'}
            className={`btn-primary text-xs px-3 py-1.5 ${
              soul.state === 'saved' ? '!bg-sage-500' :
              soul.state === 'error' ? '!bg-red-500' : ''
            }`}
          >
            {saveLabel(soul.state)}
          </button>
        </div>
        <textarea
          value={soul.content}
          onChange={e => soul.setContent(e.target.value)}
          className="w-full h-48 bg-surface-input border border-surface-border rounded-lg p-3
                     text-sm text-white/80 font-mono resize-y focus:outline-none focus:border-ocean-500/50"
          spellCheck={false}
        />
        {soul.error && <p className="text-xs text-red-400 mt-1">{soul.error}</p>}
      </section>

      {/* IDENTITY.md */}
      <section>
        <div className="flex items-center justify-between mb-2">
          <div>
            <h3 className="text-sm font-semibold text-white/80">{t('soul.identityTitle')}</h3>
            <p className="text-xs text-white/30 mt-0.5">{t('soul.identityDesc')}</p>
          </div>
          <button
            onClick={identity.save}
            disabled={!identity.dirty || identity.state === 'saving'}
            className={`btn-primary text-xs px-3 py-1.5 ${
              identity.state === 'saved' ? '!bg-sage-500' :
              identity.state === 'error' ? '!bg-red-500' : ''
            }`}
          >
            {saveLabel(identity.state)}
          </button>
        </div>
        <textarea
          value={identity.content}
          onChange={e => identity.setContent(e.target.value)}
          className="w-full h-48 bg-surface-input border border-surface-border rounded-lg p-3
                     text-sm text-white/80 font-mono resize-y focus:outline-none focus:border-ocean-500/50"
          spellCheck={false}
        />
        {identity.error && <p className="text-xs text-red-400 mt-1">{identity.error}</p>}
      </section>

      {/* Help */}
      <div className="card text-xs text-white/30 space-y-1">
        <p><strong className="text-white/50">{t('soul.tip')}:</strong> {t('soul.tipText')}</p>
        <p>{t('soul.tipChanges')}</p>
      </div>
    </div>
  );
}
