import { useState } from 'react';
import { useSelector } from 'react-redux';
import type { RootState } from '../../store';
import type { ToolCallEntry } from '../../store/chatRuntimeSlice';

export function ToolCallItem({ call }: { call: ToolCallEntry }) {
  const [expanded, setExpanded] = useState(false);
  const argsTruncated = (call.args || '').length > 120;

  return (
    <div className="flex items-start gap-2 px-3 py-2 rounded-lg bg-surface-overlay/50 border border-surface-border/50 text-xs">
      <StatusIcon status={call.status} />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="font-mono text-white/80 truncate">{call.name}</span>
          <StatusBadge status={call.status} />
        </div>
        {call.args && (
          <div
            className={`text-white/30 font-mono text-[10px] mt-0.5 ${expanded ? '' : 'truncate'}`}
            onClick={argsTruncated ? () => setExpanded(!expanded) : undefined}
            style={argsTruncated ? { cursor: 'pointer' } : undefined}
          >
            {expanded ? call.args : call.args.slice(0, 120)}
            {argsTruncated && !expanded && (
              <span className="text-ocean-400/70 ml-1">...</span>
            )}
          </div>
        )}
        {call.output && call.status === 'done' && (
          <div className="text-white/50 mt-1 line-clamp-3 font-mono text-[10px]">{call.output.slice(0, 300)}</div>
        )}
        {call.error && (
          <div className="text-red-400/70 mt-1 font-mono text-[10px]">{call.error}</div>
        )}
      </div>
    </div>
  );
}

function StatusIcon({ status }: { status: ToolCallEntry['status'] }) {
  switch (status) {
    case 'running':
      return (
        <div className="w-4 h-4 rounded-full border-2 border-ocean-400 border-t-transparent animate-spin shrink-0 mt-0.5" />
      );
    case 'done':
      return (
        <svg className="w-4 h-4 text-sage-400 shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
        </svg>
      );
    case 'error':
      return (
        <svg className="w-4 h-4 text-red-400 shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
        </svg>
      );
    default:
      return <div className="w-4 h-4 rounded-full border border-white/20 shrink-0 mt-0.5" />;
  }
}

function StatusBadge({ status }: { status: ToolCallEntry['status'] }) {
  const colors: Record<string, string> = {
    pending: 'bg-white/10 text-white/40',
    running: 'bg-ocean-500/20 text-ocean-300',
    done: 'bg-sage-500/20 text-sage-300',
    error: 'bg-red-500/20 text-red-300',
  };
  return (
    <span className={`px-1.5 py-0.5 rounded text-[10px] font-medium ${colors[status] || colors.pending}`}>
      {status}
    </span>
  );
}

const EMPTY_CALLS: never[] = [];

export function ToolTimeline({ threadId }: { threadId: string }) {
  const calls = useSelector((s: RootState) => s.chatRuntime.toolCalls[threadId] || EMPTY_CALLS);

  if (calls.length === 0) return null;

  return (
    <div className="space-y-2 px-6 py-3">
      <div className="flex items-center gap-2 text-xs text-white/30 font-medium uppercase tracking-wider">
        <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.066 2.573c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.573 1.066c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.066-2.573c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
        </svg>
        Tool calls
      </div>
      {calls.map(call => (
        <ToolCallItem key={call.id} call={call} />
      ))}
    </div>
  );
}
