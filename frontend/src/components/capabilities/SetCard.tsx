import { useState } from 'react';
import { useT } from '../../lib/i18n/I18nContext';
import type { CapabilitySet, ToolDescriptor, AgentDescriptor } from '../../services/wails';

const KIND_LABELS: Record<string, string> = {
  builtin: 'Built-in',
  extension: 'Extension',
  mcp_server: 'MCP',
  skill: 'Skill',
};

const KIND_BADGES: Record<string, string> = {
  builtin: 'bg-ocean-500/15 text-ocean-300',
  extension: 'bg-sage-500/15 text-sage-300',
  mcp_server: 'bg-coral-500/15 text-coral-300',
  skill: 'bg-purple-500/15 text-purple-300',
};

const HEALTH_COLORS: Record<string, string> = {
  ok: 'bg-green-500',
  degraded: 'bg-amber-500',
  down: 'bg-red-500',
  unknown: 'bg-gray-500',
};

interface Props {
  set: CapabilitySet;
  onConnect?: (id: string) => void;
  onDisconnect?: (id: string) => void;
  onRemove?: (id: string) => void;
  onAgentEdit?: (agent: AgentDescriptor) => void;
  onAgentDelete?: (agentId: string) => void;
}

export function SetCard({ set, onConnect, onDisconnect, onRemove, onAgentEdit, onAgentDelete }: Props) {
  const { t } = useT();
  const [expanded, setExpanded] = useState(false);
  const [tab, setTab] = useState<'tools' | 'agents'>('tools');

  return (
    <div className="card bg-surface-elevated border border-surface-border rounded-lg overflow-hidden">
      {/* Header */}
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center gap-3 px-4 py-3 hover:bg-white/5 transition-colors text-left"
      >
        <div className="flex items-center gap-2 flex-1 min-w-0">
          <span className="text-sm font-medium text-white/90 truncate">{set.name}</span>
          <span className={`text-[10px] font-medium px-1.5 py-0.5 rounded-full ${KIND_BADGES[set.kind] || 'bg-white/10 text-white/50'}`}>
            {KIND_LABELS[set.kind] || set.kind}
          </span>
        </div>
        <div className="flex items-center gap-3 text-xs text-white/40 shrink-0">
          <span title="Tools">{set.tool_count ?? (set.tools || []).length} tools</span>
          {((set.agent_count ?? (set.agents || []).length) > 0) && <span title="Agents">{set.agent_count ?? (set.agents || []).length} agents</span>}
          <span className={`w-2 h-2 rounded-full ${HEALTH_COLORS[set.health] || HEALTH_COLORS.unknown}`} />
        </div>
        <svg className={`w-4 h-4 text-white/30 transition-transform ${expanded ? 'rotate-180' : ''}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
        </svg>
      </button>

      {/* Expanded content */}
      {expanded && (
        <div className="border-t border-surface-border">
          {/* Description */}
          {set.description && (
            <p className="px-4 py-2 text-xs text-white/50">{set.description}</p>
          )}

          {/* Tab bar */}
          <div className="flex border-b border-surface-border px-4">
            <button
              onClick={() => setTab('tools')}
              className={`px-3 py-1.5 text-xs font-medium border-b-2 transition-colors ${
                tab === 'tools' ? 'border-ocean-400 text-ocean-300' : 'border-transparent text-white/40 hover:text-white/60'
              }`}
            >
              Tools ({(set.tools || []).length})
            </button>
            {(set.agents || []).length > 0 && (
              <button
                onClick={() => setTab('agents')}
                className={`px-3 py-1.5 text-xs font-medium border-b-2 transition-colors ${
                  tab === 'agents' ? 'border-ocean-400 text-ocean-300' : 'border-transparent text-white/40 hover:text-white/60'
                }`}
              >
                Agents ({(set.agents || []).length})
              </button>
            )}
          </div>

          {/* Tab content */}
          <div className="max-h-64 overflow-y-auto">
            {tab === 'tools' && (
              (set.tools || []).length === 0 ? (
                <p className="px-4 py-3 text-xs text-white/30">{t('capabilities.noTools')}</p>
              ) : (
                <div className="divide-y divide-surface-border/50">
                  {(set.tools || []).map((tool, i) => (
                    <ToolRow key={`${tool.name}-${i}`} tool={tool} t={t} />
                  ))}
                </div>
              )
            )}
            {tab === 'agents' && (
              <div className="divide-y divide-surface-border/50">
                {(set.agents || []).map((a) => (
                  <AgentRow key={a.id} agent={a} t={t} onEdit={onAgentEdit} onDelete={onAgentDelete} />
                ))}
              </div>
            )}
          </div>

          {/* Actions for MCP servers */}
          {set.kind === 'mcp_server' && (
            <div className="flex gap-2 px-4 py-2 border-t border-surface-border">
              {set.health === 'ok' ? (
                <button
                  onClick={() => onDisconnect?.(set.id)}
                  className="text-[11px] px-2 py-1 rounded bg-coral-500/15 text-coral-300 hover:bg-coral-500/25 transition-colors"
                >
                  {t('capabilities.disconnect')}
                </button>
              ) : (
                <button
                  onClick={() => onConnect?.(set.id)}
                  className="text-[11px] px-2 py-1 rounded bg-ocean-500/15 text-ocean-300 hover:bg-ocean-500/25 transition-colors"
                >
                  {t('capabilities.connect')}
                </button>
              )}
              <button
                onClick={() => onRemove?.(set.id)}
                className="text-[11px] px-2 py-1 rounded bg-white/5 text-white/40 hover:bg-white/10 hover:text-coral-300 transition-colors"
              >
                {t('capabilities.remove')}
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function ToolRow({ tool, t }: { tool: ToolDescriptor; t: (path: string, fallbackOrVars?: string | Record<string, string | number>) => string }) {
  const [open, setOpen] = useState(false);

  return (
    <div>
      <button
        onClick={() => setOpen(!open)}
        className="w-full flex items-center gap-2 px-4 py-2 hover:bg-white/5 transition-colors text-left"
      >
        <svg className={`w-3 h-3 text-white/30 transition-transform ${open ? 'rotate-90' : ''}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
        </svg>
        <span className="text-xs font-medium text-white/70 font-mono">{tool.name}</span>
      </button>
      {open && (
        <div className="px-9 pb-2">
          <p className="text-xs text-white/50">{tool.description || t('capabilities.noDescription')}</p>
          <div className="flex gap-1.5 mt-1 flex-wrap">
            {tool.permission && tool.permission !== 'none' && (
              <span className="text-[10px] px-1 py-0.5 rounded bg-white/5 text-white/40">{tool.permission}</span>
            )}
            {tool.has_side_effects && (
              <span className="text-[10px] px-1 py-0.5 rounded bg-amber-500/10 text-amber-400">{t('capabilities.sideEffects')}</span>
            )}
            {tool.is_concurrency_safe && (
              <span className="text-[10px] px-1 py-0.5 rounded bg-green-500/10 text-green-400">{t('capabilities.concurrent')}</span>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function AgentRow({ agent, t, onEdit, onDelete }: {
  agent: AgentDescriptor;
  t: (path: string, fallbackOrVars?: string | Record<string, string | number>) => string;
  onEdit?: (agent: AgentDescriptor) => void;
  onDelete?: (agentId: string) => void;
}) {
  const [open, setOpen] = useState(false);

  return (
    <div>
      <div className="flex items-center">
        <button
          onClick={() => setOpen(!open)}
          className="flex-1 flex items-center gap-2 px-4 py-2 hover:bg-white/5 transition-colors text-left"
        >
          <svg className={`w-3 h-3 text-white/30 transition-transform ${open ? 'rotate-90' : ''}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
          </svg>
          <span className="text-xs font-medium text-white/70">{agent.name}</span>
          <span className="text-[10px] text-white/40">{agent.tier}</span>
          {agent.hidden && <span className="text-[10px] text-white/30">{t('capabilities.hidden')}</span>}
          {agent.background && <span className="text-[10px] text-white/30">{t('capabilities.background')}</span>}
        </button>
        {onEdit && (
          <button onClick={() => onEdit(agent)} className="px-2 py-1 text-[10px] text-white/30 hover:text-white/60 transition-colors"
            title="Edit agent">
            <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
            </svg>
          </button>
        )}
      </div>
      {open && (
        <div className="px-9 pb-2">
          <p className="text-xs text-white/50">{agent.description || t('capabilities.noDescription')}</p>
          <div className="flex gap-1.5 mt-1 flex-wrap">
            {agent.model && <span className="text-[10px] px-1 py-0.5 rounded bg-white/5 text-white/40">{agent.model}</span>}
            {agent.max_iterations > 0 && <span className="text-[10px] px-1 py-0.5 rounded bg-white/5 text-white/40">{t('capabilities.maxIter', { count: agent.max_iterations })}</span>}
            {agent.sandbox_mode && <span className="text-[10px] px-1 py-0.5 rounded bg-white/5 text-white/40">{agent.sandbox_mode}</span>}
          </div>
          {agent.tool_allowlist && agent.tool_allowlist.length > 0 && (
            <div className="mt-1.5 flex flex-wrap gap-1">
              {agent.tool_allowlist.slice(0, 8).map(t => (
                <span key={t} className="text-[10px] px-1 py-0.5 rounded bg-ocean-500/10 text-ocean-300/70 font-mono">{t}</span>
              ))}
              {agent.tool_allowlist.length > 8 && (
                <span className="text-[10px] text-white/30">+{agent.tool_allowlist.length - 8} more</span>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
