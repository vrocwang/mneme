import { useState, useEffect } from 'react';
import * as api from '../../services/wails';
import { useT } from '../../lib/i18n/I18nContext';

export function KeyringConsentOverlay() {
  const { t } = useT();
  const [visible, setVisible] = useState(false);
  const [status, setStatus] = useState<Record<string, unknown> | null>(null);

  useEffect(() => {
    api.keyringStatus().then(r => {
      if (r) {
        setStatus(r);
        if (r.activeMode === 'consent_pending') setVisible(true);
      }
    }).catch(() => {});
  }, []);

  if (!visible) return null;

  const decide = async (mode: string) => {
    try { await api.keyringConsentDecide(mode); } catch {}
    setVisible(false);
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className="bg-surface-elevated border border-surface-border rounded-xl p-6 max-w-md mx-4 shadow-2xl">
        <div className="flex items-center gap-3 mb-3">
          <div className="w-9 h-9 rounded-xl bg-amber-500/15 flex items-center justify-center shrink-0">
            <svg className="w-5 h-5 text-amber-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
            </svg>
          </div>
          <h3 className="text-sm font-semibold text-white/90">
            {t('keyring.consentTitle') || 'Storage Authorization'}
          </h3>
        </div>

        <p className="text-xs text-white/50 mb-3 leading-relaxed">
          {t('keyring.consentBody') || 'The system keyring is not available. Would you like to store secrets in an encrypted local file instead?'}
        </p>

        {status && (
          <div className="text-[10px] text-white/30 mb-4 space-y-0.5 font-mono bg-surface border border-surface-border rounded-lg px-3 py-2">
            <p>Backend: {String(status.backendName || 'file')} · Mode: {String(status.activeMode || 'consent_pending')}</p>
            {status.failureReason ? <p className="text-amber-400/70">{String(status.failureReason)}</p> : null}
          </div>
        )}

        <div className="flex gap-2 justify-end">
          <button
            onClick={() => decide('declined')}
            className="text-xs px-4 py-2 rounded-lg border border-surface-border text-white/50 hover:text-white/70 hover:bg-white/5 transition-colors"
          >
            {t('keyring.consentDecline') || 'Decline'}
          </button>
          <button
            onClick={() => decide('local_encrypted')}
            className="text-xs px-4 py-2 rounded-lg bg-ocean-500 text-white hover:bg-ocean-600 transition-colors"
          >
            {t('keyring.consentAllow') || 'Allow Local Storage'}
          </button>
        </div>
      </div>
    </div>
  );
}
