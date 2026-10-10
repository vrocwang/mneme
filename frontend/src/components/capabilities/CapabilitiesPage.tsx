import { useState, useEffect, useCallback } from 'react';
import { useT } from '../../lib/i18n/I18nContext';
import * as api from '../../services/wails';
import type { CapabilitySet } from '../../services/wails';
import { SetCard } from './SetCard';

const KIND_GROUPS: { kind: string; labelKey: string }[] = [
  { kind: 'builtin', labelKey: 'capabilities.kindBuiltin' },
  { kind: 'extension', labelKey: 'capabilities.groupExtensions' },
  { kind: 'mcp_server', labelKey: 'capabilities.groupMcpServers' },
  { kind: 'skill', labelKey: 'capabilities.groupSkills' },
];

export function CapabilitiesPage() {
  const { t } = useT();
  const [sets, setSets] = useState<CapabilitySet[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [activeKind, setActiveKind] = useState<string>('all');
  const [showInstall, setShowInstall] = useState(false);
  const [mcpForm, setMcpForm] = useState({ name: '', transport: 'stdio', command: '', url: '', args: '' });
  const [skillUrl, setSkillUrl] = useState('');
  const [skillMsg, setSkillMsg] = useState('');
  const [skillOk, setSkillOk] = useState(false);
  const [installing, setInstalling] = useState(false);
  const [showAgentEditor, setShowAgentEditor] = useState(false);
  const [editingAgent, setEditingAgent] = useState<api.AgentInfo | null>(null);
  const [agentForm, setAgentForm] = useState({ id: '', name: '', description: '', tier: 'worker', model: '', toolAllowlist: '', maxIterations: 10, systemPrompt: '' });

  const fetchSets = useCallback(async () => {
    try {
      setError(null);
      const data = await api.listCapabilitySets();
      setSets(data || []);
    } catch (e) {
      setError(e instanceof Error ? e.message : t('errors.loadCapabilities'));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => { fetchSets(); }, [fetchSets]);

  const handleConnect = async (setId: string) => {
    const name = setId.replace(/^mcp:/, '');
    try {
      await api.connectCapMCPServer(name);
      await fetchSets();
    } catch (e) {
      setError(e instanceof Error ? e.message : t('errors.connectFailed'));
    }
  };

  const handleDisconnect = async (setId: string) => {
    const name = setId.replace(/^mcp:/, '');
    try {
      await api.disconnectCapMCPServer(name);
      await fetchSets();
    } catch (e) {
      setError(e instanceof Error ? e.message : t('errors.disconnectFailed'));
    }
  };

  const handleRemove = async (setId: string) => {
    const name = setId.replace(/^mcp:/, '');
    try {
      await api.removeMCPServer(name);
      await fetchSets();
    } catch (e) {
      setError(e instanceof Error ? e.message : t('errors.removeFailed'));
    }
  };

  const handleMcpInstall = async () => {
    if (!mcpForm.name.trim()) return;
    setInstalling(true);
    try {
      await api.addMCPServer(
        mcpForm.name,
        mcpForm.transport,
        mcpForm.transport === 'stdio' ? mcpForm.command : '',
        mcpForm.transport === 'http' ? mcpForm.url : '',
        mcpForm.transport === 'stdio' && mcpForm.args ? mcpForm.args.split(',').map(s => s.trim()).filter(Boolean) : [],
      );
      setMcpForm({ name: '', transport: 'stdio', command: '', url: '', args: '' });
      setShowInstall(false);
      await fetchSets();
    } catch (e) {
      setError(e instanceof Error ? e.message : t('errors.installFailed'));
    } finally {
      setInstalling(false);
    }
  };

  const openAgentCreate = () => {
    setEditingAgent(null);
    setAgentForm({ id: '', name: '', description: '', tier: 'worker', model: '', toolAllowlist: '', maxIterations: 10, systemPrompt: '' });
    setShowAgentEditor(true);
  };

  const openAgentEdit = (agent: api.AgentInfo) => {
    setEditingAgent(agent);
    setAgentForm({
      id: agent.id, name: agent.name, description: agent.description || '',
      tier: agent.tier || 'worker', model: agent.model || '',
      toolAllowlist: agent.toolAllowlist?.join(', ') || '',
      maxIterations: agent.maxIterations || 10,
      systemPrompt: agent.systemPrompt || '',
    });
    setShowAgentEditor(true);
  };

  const handleAgentSave = async () => {
    if (!agentForm.id.trim() || !agentForm.name.trim()) return;
    setInstalling(true);
    try {
      const allowlist = agentForm.toolAllowlist
        .split(',')
        .map(t => t.trim())
        .filter(t => t !== '');
      await api.upsertAgent(
        agentForm.id.trim(),
        agentForm.name.trim(),
        agentForm.description.trim(),
        agentForm.systemPrompt.trim(),
        agentForm.tier,
        agentForm.model.trim(),
        agentForm.maxIterations,
        allowlist,
      );
      setShowAgentEditor(false);
      await fetchSets();
    } catch (e) {
      setError(e instanceof Error ? e.message : t('errors.agentSaveFailed'));
    } finally {
      setInstalling(false);
    }
  };

  const handleSkillInstall = async () => {
    const url = skillUrl.trim();
    if (!url) return;
    setInstalling(true);
    setSkillMsg('');
    setSkillOk(false);
    try {
      await api.installSkill(url);
      setSkillUrl('');
      setSkillMsg(t('capabilities.skillInstalled'));
      setSkillOk(true);
      await fetchSets();
    } catch (e) {
      setSkillMsg(e instanceof Error ? e.message : t('errors.installFailed'));
    } finally {
      setInstalling(false);
    }
  };

  const handleAgentDelete = async (id: string) => {
    if (!confirm(t('capabilities.deleteAgentConfirm', { id }))) return;
    try {
      await api.removeAgent(id);
      await fetchSets();
    } catch (e) {
      setError(e instanceof Error ? e.message : t('errors.agentDeleteFailed'));
    }
  };

  const filteredSets = activeKind === 'all'
    ? sets
    : sets.filter(s => s.kind === activeKind);

  const totalTools = sets.reduce((sum, s) => sum + s.tool_count, 0);
  const totalAgents = sets.reduce((sum, s) => sum + s.agent_count, 0);

  return (
    <div className="flex flex-col h-full overflow-hidden">
      {/* Header */}
      <div className="shrink-0 px-6 py-4 border-b border-surface-border">
        <div className="flex items-center justify-between mb-3">
          <div>
            <h1 className="text-lg font-semibold text-white/90">{t('capabilities.title')}</h1>
            <p className="text-xs text-white/40 mt-0.5">
              {t('capabilities.summary', { tools: totalTools, agents: totalAgents, sources: sets.length })}
            </p>
          </div>
          <div className="flex items-center gap-2">
            {(activeKind === 'all' || activeKind === 'builtin') && (
              <button
                onClick={openAgentCreate}
                className="text-xs px-3 py-1.5 rounded-md bg-sage-500/15 text-sage-300 hover:bg-sage-500/25 transition-colors"
              >
                {t('capabilities.addAgent')}
              </button>
            )}
            {(activeKind === 'all' || activeKind === 'mcp_server') && (
              <button
                onClick={() => setShowInstall(true)}
                className="text-xs px-3 py-1.5 rounded-md bg-ocean-500 text-white hover:bg-ocean-600 transition-colors"
              >
                + {t('capabilities.installServer')}
              </button>
            )}
          </div>
        </div>

        {/* Kind filter tabs */}
        <div className="flex gap-1 flex-wrap">
          <button
            onClick={() => setActiveKind('all')}
            className={`text-[11px] px-2.5 py-1 rounded-full transition-colors ${
              activeKind === 'all' ? 'bg-white/10 text-white/80' : 'text-white/40 hover:text-white/60'
            }`}
          >
            {t('capabilities.allTab', { count: sets.length })}
          </button>
          {KIND_GROUPS.map(({ kind, labelKey }) => {
            const count = sets.filter(s => s.kind === kind).length;
            return (
              <button
                key={kind}
                onClick={() => setActiveKind(kind)}
                className={`text-[11px] px-2.5 py-1 rounded-full transition-colors ${
                  activeKind === kind ? 'bg-white/10 text-white/80' : 'text-white/40 hover:text-white/60'
                }`}
              >
                {t(labelKey)} ({count})
              </button>
            );
          })}
        </div>
      </div>

      {/* Error toast */}
      {error && (
        <div className="shrink-0 mx-6 mt-3 flex items-center gap-2 px-3 py-2 rounded bg-coral-500/10 border border-coral-500/20">
          <span className="text-xs text-coral-400 flex-1">{error}</span>
          <button onClick={() => setError(null)} className="text-coral-400 hover:text-coral-300">
            <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>
      )}

      {/* Extension hint */}
      {activeKind === 'extension' && (
        <div className="shrink-0 mx-6 mt-3 px-3 py-2 rounded bg-surface-elevated border border-surface-border">
          <p className="text-xs text-white/40">
            {t('capabilities.extensionsManaged')}
          </p>
        </div>
      )}

      {/* Skill install form */}
      {activeKind === 'skill' && (
        <div className="shrink-0 mx-6 mt-3 px-3 py-2 rounded bg-surface-elevated border border-surface-border">
          <p className="text-xs text-white/40 mb-2">
            {t('capabilities.skillInstallHint')}
          </p>
          <div className="flex gap-2">
            <input
              type="text"
              value={skillUrl}
              onChange={e => setSkillUrl(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && handleSkillInstall()}
              placeholder={t('capabilities.skillUrlPlaceholder')}
              className="flex-1 text-xs px-2 py-1.5 rounded bg-black/30 border border-white/10 text-white/70 placeholder-white/20 focus:outline-none focus:border-ocean-500/50"
            />
            <button
              onClick={handleSkillInstall}
              disabled={installing || !skillUrl.trim()}
              className="text-xs px-3 py-1.5 rounded-md bg-ocean-500 text-white hover:bg-ocean-600 disabled:opacity-40 transition-colors shrink-0"
            >
              {installing ? t('capabilities.installing') : t('extensions.install')}
            </button>
          </div>
          {skillMsg && (
            <p className={`text-xs mt-1.5 ${skillOk ? 'text-sage-400' : 'text-coral-400'}`}>
              {skillMsg}
            </p>
          )}
        </div>
      )}

      {/* Set list */}
      <div className="flex-1 overflow-y-auto px-6 py-4">
        {loading ? (
          <div className="flex items-center justify-center py-16">
            <div className="w-5 h-5 border-2 border-ocean-400/30 border-t-ocean-400 rounded-full animate-spin" />
          </div>
        ) : filteredSets.length === 0 ? (
          <div className="text-center py-16">
            <p className="text-sm text-white/30">{t('capabilities.noSets')}</p>
          </div>
        ) : (
          <div className="flex flex-col gap-2">
            {filteredSets.map(s => (
              <SetCard
                key={s.id}
                set={s}
                onConnect={s.kind === 'mcp_server' ? handleConnect : undefined}
                onDisconnect={s.kind === 'mcp_server' ? handleDisconnect : undefined}
                onRemove={s.kind === 'mcp_server' ? handleRemove : undefined}
                onAgentEdit={(agent) => {
                  const info: api.AgentInfo = {
                    id: agent.id, name: agent.name, description: agent.description || '',
                    tier: agent.tier, model: agent.model,
                    toolAllowlist: agent.tool_allowlist, toolDenylist: agent.tool_denylist,
                    maxIterations: agent.max_iterations,
                    hidden: agent.hidden, sandboxMode: agent.sandbox_mode,
                  };
                  openAgentEdit(info);
                }}
              />
            ))}
          </div>
        )}
      </div>

      {/* Agent Editor modal */}
      {showAgentEditor && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={() => setShowAgentEditor(false)}>
          <div className="bg-surface-elevated border border-surface-border rounded-lg w-full max-w-lg mx-4 p-5 max-h-[85vh] overflow-y-auto" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-sm font-semibold text-white/90">
                {editingAgent ? t('capabilities.editAgent', { name: editingAgent.name }) : t('capabilities.createAgent')}
              </h2>
              <button onClick={() => setShowAgentEditor(false)} className="text-white/30 hover:text-white/60">
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            </div>
            <div className="flex flex-col gap-3">
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="text-[11px] text-white/40 block mb-1">{t('capabilities.labelId')}</label>
                  <input className="input-field w-full bg-surface border border-surface-border rounded px-2.5 py-1.5 text-xs text-white/80 font-mono"
                    value={agentForm.id} onChange={e => setAgentForm({ ...agentForm, id: e.target.value })}
                    placeholder={t('capabilities.idPlaceholder')} disabled={!!editingAgent} />
                </div>
                <div>
                  <label className="text-[11px] text-white/40 block mb-1">{t('settings.name')}</label>
                  <input className="input-field w-full bg-surface border border-surface-border rounded px-2.5 py-1.5 text-xs text-white/80"
                    value={agentForm.name} onChange={e => setAgentForm({ ...agentForm, name: e.target.value })}
                    placeholder={t('capabilities.namePlaceholder')} />
                </div>
              </div>
              <div>
                <label className="text-[11px] text-white/40 block mb-1">{t('capabilities.labelDescription')}</label>
                <input className="input-field w-full bg-surface border border-surface-border rounded px-2.5 py-1.5 text-xs text-white/80"
                  value={agentForm.description} onChange={e => setAgentForm({ ...agentForm, description: e.target.value })}
                  placeholder={t('capabilities.descriptionPlaceholder')} />
              </div>
              <div className="grid grid-cols-3 gap-3">
                <div>
                  <label className="text-[11px] text-white/40 block mb-1">{t('capabilities.labelTier')}</label>
                  <select className="input-field w-full bg-surface border border-surface-border rounded px-2.5 py-1.5 text-xs text-white/80"
                    value={agentForm.tier} onChange={e => setAgentForm({ ...agentForm, tier: e.target.value })}>
                    <option value="chat">chat</option>
                    <option value="reasoning">reasoning</option>
                    <option value="worker">worker</option>
                  </select>
                </div>
                <div>
                  <label className="text-[11px] text-white/40 block mb-1">{t('capabilities.labelModelOptional')}</label>
                  <input className="input-field w-full bg-surface border border-surface-border rounded px-2.5 py-1.5 text-xs text-white/80"
                    value={agentForm.model} onChange={e => setAgentForm({ ...agentForm, model: e.target.value })}
                    placeholder={t('capabilities.modelPlaceholder')} />
                </div>
                <div>
                  <label className="text-[11px] text-white/40 block mb-1">{t('capabilities.labelMaxIterations')}</label>
                  <input className="input-field w-full bg-surface border border-surface-border rounded px-2.5 py-1.5 text-xs text-white/80"
                    type="number" min={1} max={50}
                    value={agentForm.maxIterations} onChange={e => setAgentForm({ ...agentForm, maxIterations: Number(e.target.value) })} />
                </div>
              </div>
              <div>
                <label className="text-[11px] text-white/40 block mb-1">{t('capabilities.labelToolAllowlist')}</label>
                <input className="input-field w-full bg-surface border border-surface-border rounded px-2.5 py-1.5 text-xs text-white/80 font-mono"
                  value={agentForm.toolAllowlist} onChange={e => setAgentForm({ ...agentForm, toolAllowlist: e.target.value })}
                  placeholder={t('capabilities.toolAllowlistPlaceholder')} />
              </div>
              <div>
                <label className="text-[11px] text-white/40 block mb-1">{t('capabilities.labelSystemPrompt')}</label>
                <textarea className="input-field w-full bg-surface border border-surface-border rounded px-2.5 py-1.5 text-xs text-white/80 font-mono h-24 resize-y"
                  value={agentForm.systemPrompt} onChange={e => setAgentForm({ ...agentForm, systemPrompt: e.target.value })}
                  placeholder={t('capabilities.systemPromptPlaceholder')} />
              </div>
              <div className="flex justify-between gap-2 mt-2">
                <div>
                  {editingAgent && (
                    <button onClick={() => { handleAgentDelete(editingAgent.id); setShowAgentEditor(false); }}
                      className="text-xs px-3 py-1.5 rounded bg-coral-500/15 text-coral-300 hover:bg-coral-500/25 transition-colors">
                      {t('settings.delete')}
                    </button>
                  )}
                </div>
                <div className="flex gap-2">
                  <button onClick={() => setShowAgentEditor(false)}
                    className="text-xs px-3 py-1.5 rounded text-white/40 hover:text-white/60 transition-colors">{t('settings.cancel')}</button>
                  <button onClick={handleAgentSave}
                    disabled={installing || !agentForm.id.trim() || !agentForm.name.trim()}
                    className="text-xs px-4 py-1.5 rounded-md bg-ocean-500 text-white hover:bg-ocean-600 disabled:opacity-40 transition-colors">
                    {installing ? t('settings.saving') : editingAgent ? t('settings.update') : t('capabilities.createAgentBtn')}
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Install MCP Server modal */}
      {showInstall && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={() => setShowInstall(false)}>
          <div className="bg-surface-elevated border border-surface-border rounded-lg w-full max-w-md mx-4 p-5" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-sm font-semibold text-white/90">{t('capabilities.installTitle')}</h2>
              <button onClick={() => setShowInstall(false)} className="text-white/30 hover:text-white/60">
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            </div>
            <div className="flex flex-col gap-3">
              <div>
                <label className="text-[11px] text-white/40 block mb-1">{t('capabilities.name')}</label>
                <input
                  className="input-field w-full bg-surface border border-surface-border rounded px-2.5 py-1.5 text-xs text-white/80"
                  value={mcpForm.name}
                  onChange={e => setMcpForm({ ...mcpForm, name: e.target.value })}
                  placeholder={t('capabilities.serverNamePlaceholder')}
                />
              </div>
              <div>
                <label className="text-[11px] text-white/40 block mb-1">{t('capabilities.transport')}</label>
                <select
                  className="input-field w-full bg-surface border border-surface-border rounded px-2.5 py-1.5 text-xs text-white/80"
                  value={mcpForm.transport}
                  onChange={e => setMcpForm({ ...mcpForm, transport: e.target.value })}
                >
                  <option value="stdio">stdio</option>
                  <option value="http">http</option>
                </select>
              </div>
              {mcpForm.transport === 'stdio' ? (
                <>
                  <div>
                    <label className="text-[11px] text-white/40 block mb-1">{t('capabilities.command')}</label>
                    <input
                      className="input-field w-full bg-surface border border-surface-border rounded px-2.5 py-1.5 text-xs text-white/80"
                      value={mcpForm.command}
                      onChange={e => setMcpForm({ ...mcpForm, command: e.target.value })}
                      placeholder={t('capabilities.commandPlaceholder')}
                    />
                  </div>
                  <div>
                    <label className="text-[11px] text-white/40 block mb-1">{t('capabilities.args')}</label>
                    <input
                      className="input-field w-full bg-surface border border-surface-border rounded px-2.5 py-1.5 text-xs text-white/80"
                      value={mcpForm.args}
                      onChange={e => setMcpForm({ ...mcpForm, args: e.target.value })}
                      placeholder={t('capabilities.argsPlaceholder')}
                    />
                  </div>
                </>
              ) : (
                <div>
                  <label className="text-[11px] text-white/40 block mb-1">{t('capabilities.url')}</label>
                  <input
                    className="input-field w-full bg-surface border border-surface-border rounded px-2.5 py-1.5 text-xs text-white/80"
                    value={mcpForm.url}
                    onChange={e => setMcpForm({ ...mcpForm, url: e.target.value })}
                    placeholder={t('capabilities.urlPlaceholder')}
                  />
                </div>
              )}
              <div className="flex justify-end gap-2 mt-2">
                <button onClick={() => setShowInstall(false)} className="text-xs px-3 py-1.5 rounded text-white/40 hover:text-white/60 transition-colors">
                  {t('capabilities.cancel')}
                </button>
                <button
                  onClick={handleMcpInstall}
                  disabled={installing || !mcpForm.name.trim()}
                  className="btn-primary text-xs px-4 py-1.5 rounded-md bg-ocean-500 text-white hover:bg-ocean-600 disabled:opacity-40 transition-colors"
                >
                  {installing ? t('capabilities.installing') : t('capabilities.installConnect')}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
