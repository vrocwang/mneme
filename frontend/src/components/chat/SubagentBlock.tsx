import { useT } from '../../lib/i18n/I18nContext';

export interface SubagentInfo {
  id: string;
  agentType: string;
  task: string;
  status: 'running' | 'completed' | 'failed';
  output?: string;
}

export function SubagentBlock({ subagent }: { subagent: SubagentInfo }) {
  const { t } = useT();

  const statusColors: Record<SubagentInfo['status'], string> = {
    running: 'bg-ocean-500/20 text-ocean-400 border-ocean-500/30',
    completed: 'bg-sage-500/20 text-sage-400 border-sage-500/30',
    failed: 'bg-coral-500/20 text-coral-400 border-coral-500/30',
  };

  const statusLabel: Record<SubagentInfo['status'], string> = {
    running: t('chat.subagentRunning'),
    completed: t('chat.subagentCompleted'),
    failed: t('chat.subagentFailed'),
  };

  return (
    <div className={`rounded-xl border px-4 py-3 text-sm ${statusColors[subagent.status]}`}>
      <div className="flex items-center gap-2 mb-1">
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M17 20h5v-2a3 3 0 00-5.356-1.857M17 20H7m10 0v-2c0-.656-.126-1.283-.356-1.857M7 20H2v-2a3 3 0 015.356-1.857M7 20v-2c0-.656.126-1.283.356-1.857m0 0a5.002 5.002 0 019.288 0M15 7a3 3 0 11-6 0 3 3 0 016 0z" />
        </svg>
        <span className="font-medium">
          {t('chat.subagentLabel', { type: subagent.agentType })}
        </span>
        {subagent.status === 'running' && (
          <span className="w-2 h-2 rounded-full bg-ocean-400 animate-pulse ml-auto" />
        )}
        <span className="text-xs opacity-70 ml-auto">{statusLabel[subagent.status]}</span>
      </div>
      <p className="text-xs opacity-70 line-clamp-2">{subagent.task}</p>
      {subagent.output && subagent.status === 'completed' && (
        <div className="mt-2 pt-2 border-t border-current/10">
          <p className="text-xs opacity-80 line-clamp-3 whitespace-pre-wrap">{subagent.output}</p>
        </div>
      )}
    </div>
  );
}
