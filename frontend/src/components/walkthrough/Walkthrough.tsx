import { useState, useEffect, useCallback } from 'react';
import { useT } from '../../lib/i18n/I18nContext';
import { StorageKeys, loadFlag, saveFlag } from '../../lib/storage';
import { navSelector } from '../../lib/routes';
import type { RouteId } from '../../lib/routes';

type StepKey = Extract<RouteId, 'chat' | 'memory' | 'capabilities' | 'settings'>;

const steps: StepKey[] = ['chat', 'memory', 'capabilities', 'settings'];
const selectors: Record<StepKey, string> = {
  chat:         navSelector('chat'),
  memory:       navSelector('memory'),
  capabilities: navSelector('capabilities'),
  settings:     navSelector('settings'),
};

// Tooltip geometry. TOOLTIP_WIDTH must stay in sync with the `w-[260px]`
// class on the tooltip element (Tailwind only sees the literal).
const TOOLTIP_WIDTH = 260;
const TOOLTIP_GAP = 14;
const TOOLTIP_MIN_TOP = 12;
const TOOLTIP_CENTER_OFFSET = 60;
// The sidebar needs a frame to lay out before it can be measured.
const RENDER_SETTLE_MS = 200;

export function Walkthrough() {
  const { t } = useT();
  const [step, setStep] = useState(0);
  const [dismissed, setDismissed] = useState(() => {
    if (loadFlag(StorageKeys.walkthroughDone)) return true;
    if (!loadFlag(StorageKeys.onboardingDone)) return true; // wait for onboarding
    return false;
  });
  const [pos, setPos] = useState({ top: 0, left: 0, side: 'right' as 'left' | 'right' });

  const measure = useCallback(() => {
    const el = document.querySelector(selectors[steps[step]]);
    if (!el) return;
    const r = el.getBoundingClientRect();
    const tooltipW = TOOLTIP_WIDTH;
    const gap = TOOLTIP_GAP;
    const vw = window.innerWidth;
    const rightSide = r.right + gap + tooltipW < vw;
    setPos({
      top: Math.max(TOOLTIP_MIN_TOP, r.top + r.height / 2 - TOOLTIP_CENTER_OFFSET),
      left: rightSide ? r.right + gap : r.left - gap - tooltipW,
      side: rightSide ? 'left' : 'right',
    });
  }, [step]);

  useEffect(() => {
    if (dismissed) return;
    const timer = setTimeout(measure, RENDER_SETTLE_MS);
    window.addEventListener('resize', measure);
    return () => { clearTimeout(timer); window.removeEventListener('resize', measure); };
  }, [dismissed, measure]);

  if (dismissed) return null;

  const key = steps[step];

  const next = () => {
    if (step < steps.length - 1) setStep(step + 1);
    else dismiss();
  };
  const prev = () => { if (step > 0) setStep(step - 1); };
  const dismiss = () => { saveFlag(StorageKeys.walkthroughDone, true); setDismissed(true); };

  return (
    <div className="fixed inset-0 z-50 pointer-events-none">
      {/* Semi-transparent backdrop — clicking dismisses */}
      <div className="absolute inset-0 bg-black/20 pointer-events-auto" onClick={dismiss} />

      {/* Tooltip positioned next to the target element */}
      <div
        className="absolute pointer-events-auto w-[260px] bg-surface border border-surface-border rounded-xl shadow-2xl p-4 animate-fade-in"
        style={{ top: pos.top, left: pos.left }}
      >
        {/* Arrow pointing toward the sidebar */}
        {pos.side === 'left' && (
          <div className="absolute top-14 -left-1.5 w-3 h-3 bg-surface border-l border-t border-surface-border rotate-[-45deg]" />
        )}
        {pos.side === 'right' && (
          <div className="absolute top-14 -right-1.5 w-3 h-3 bg-surface border-r border-t border-surface-border rotate-45" />
        )}

        <div className="flex items-center justify-between mb-2">
          <span className="text-xs text-white/30">{t('walkthrough.step', { current: String(step + 1), total: String(steps.length) })}</span>
          <button onClick={dismiss} className="text-xs text-white/40 hover:text-white/70">{t('walkthrough.skip')}</button>
        </div>
        <h4 className="text-sm font-semibold text-white/80 mb-1">{t(`walkthrough.steps.${key}.title`)}</h4>
        <p className="text-sm text-white/50 mb-3">{t(`walkthrough.steps.${key}.content`)}</p>
        <div className="flex justify-between">
          <button onClick={prev} disabled={step === 0} className="text-xs px-3 py-1 rounded border border-white/10 text-white/50 disabled:opacity-30 hover:text-white/70">{t('walkthrough.back')}</button>
          <button onClick={next} className="text-xs px-3 py-1 rounded bg-ocean-500 text-white hover:bg-ocean-600">
            {step === steps.length - 1 ? t('walkthrough.gotIt') : t('walkthrough.next')}
          </button>
        </div>
      </div>
    </div>
  );
}
