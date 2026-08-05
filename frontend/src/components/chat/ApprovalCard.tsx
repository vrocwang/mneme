import type { PendingApproval } from '../../services/wails';
import { useT } from '../../lib/i18n/I18nContext';

interface Props {
  approval: PendingApproval;
  onDecide: (id: string, decision: 'approve_once' | 'approve_always' | 'deny') => void;
}

export function ApprovalCard({ approval, onDecide }: Props) {
  const { t } = useT();

  return (
    <div className="animate-slide-up card border-amber-500/30 bg-amber-500/5">
      <div className="flex items-start gap-3">
        <div className="w-8 h-8 rounded-lg bg-amber-500/20 flex items-center justify-center shrink-0 mt-0.5">
          <svg className="w-4 h-4 text-amber-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
          </svg>
        </div>
        <div className="flex-1 min-w-0">
          <h3 className="text-sm font-semibold text-amber-300">{t('approval.required')}</h3>
          <p className="text-xs text-white/50 mt-0.5">
            <code className="text-amber-400/80 bg-amber-500/10 px-1 py-0.5 rounded text-xs font-mono break-all">{approval.tool_name}</code>
            {' '}{t('approval.wantsToExecute')}
          </p>
          {approval.reason && (
            <p className="text-xs text-white/40 mt-1">{approval.reason}</p>
          )}
          {approval.args && approval.args !== '{}' && (
            <pre className="mt-2 text-xs text-white/30 bg-surface/50 rounded-lg p-2 overflow-x-auto max-h-24 font-mono">
              {approval.args.length > 300 ? approval.args.slice(0, 300) + '...' : approval.args}
            </pre>
          )}
          <div className="flex gap-2 mt-3">
            <button className="btn-success text-xs !py-1.5 !px-3" onClick={() => onDecide(approval.id, 'approve_once')}>
              {t('approval.approveOnce')}
            </button>
            <button className="btn-success text-xs !py-1.5 !px-3 !bg-sage-600/30" onClick={() => onDecide(approval.id, 'approve_always')}>
              {t('approval.alwaysAllow')}
            </button>
            <button className="btn-danger text-xs !py-1.5 !px-3" onClick={() => onDecide(approval.id, 'deny')}>
              {t('approval.deny')}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
