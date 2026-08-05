import { useState } from 'react';
import { useT } from '../../lib/i18n/I18nContext';

export function ThinkingBlock({ content }: { content: string }) {
  const { t } = useT();
  const [open, setOpen] = useState(false);

  return (
    <div className="my-2 rounded-lg border border-ocean-500/20 bg-ocean-500/5 overflow-hidden">
      <button
        onClick={() => setOpen(!open)}
        className="w-full flex items-center gap-2 px-3 py-2 text-xs text-ocean-400/80 hover:text-ocean-300 transition-colors"
      >
        <svg
          className={`w-3 h-3 transition-transform ${open ? 'rotate-90' : ''}`}
          fill="none" stroke="currentColor" viewBox="0 0 24 24"
        >
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
        </svg>
        <span>{open ? t('chat.thinkingHide') : t('chat.thinkingShow')}</span>
        <span className="ml-auto font-mono text-white/20">···</span>
      </button>
      {open && (
        <div className="px-3 pb-2 text-xs text-white/60 font-mono leading-relaxed whitespace-pre-wrap">
          {content.trim()}
        </div>
      )}
    </div>
  );
}
