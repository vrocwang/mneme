import { useState, useEffect } from 'react';
import * as api from '../../services/wails';
import { useT } from '../../lib/i18n/I18nContext';
import type {
  AgentLimitsConfig, SecurityCommandsConfig, ShellConfig,
  CircuitBreakerConfig, MemoryPipelineConfig, RetrievalWeightsConfig, CostConfig,
} from '../../services/wails';

type Section = 'agent' | 'security' | 'shell' | 'memory' | 'breaker' | 'cost' | 'paths';

const SECTIONS: { id: Section; key: string }[] = [
  { id: 'agent', key: 'settings.agentLimits' },
  { id: 'security', key: 'settings.security' },
  { id: 'shell', key: 'settings.shellTool' },
  { id: 'memory', key: 'settings.memoryPipeline' },
  { id: 'breaker', key: 'settings.circuitBreaker' },
  { id: 'cost', key: 'settings.costBudget' },
  { id: 'paths', key: 'settings.dataDir' },
];

const WEIGHT_KEYS = ['fts5', 'vector', 'keyword', 'tree', 'graph', 'episodic'] as const;

function SectionHeader({ title, desc }: { title: string; desc: string }) {
  return (
    <div className="mb-4">
      <h3 className="text-sm font-semibold text-white/80 break-words">{title}</h3>
      <p className="text-xs text-white/30 mt-0.5 break-words">{desc}</p>
    </div>
  );
}

function Toggle({ label, value, onChange, desc }: { label: string; value: boolean; onChange: (v: boolean) => void; desc?: string }) {
  return (
    <div className="flex items-center justify-between py-2 gap-3">
      <div className="min-w-0 flex-1">
        <span className="text-sm text-white/70 break-words">{label}</span>
        {desc && <p className="text-xs text-white/25 mt-0.5 break-words">{desc}</p>}
      </div>
      <button onClick={() => onChange(!value)} className={`w-10 h-5 rounded-full transition-colors shrink-0 ${value ? 'bg-ocean-500' : 'bg-surface-muted'}`}>
        <div className={`w-4 h-4 rounded-full bg-white transition-transform ${value ? 'translate-x-5' : 'translate-x-0.5'}`} />
      </button>
    </div>
  );
}

function NumberInput({ label, value, onChange, min, max, unit }: {
  label: string; value: number; onChange: (v: number) => void; min?: number; max?: number; unit?: string;
}) {
  return (
    <div className="flex items-center justify-between py-2 gap-3 min-w-0">
      <span className="text-sm text-white/70 truncate min-w-0 shrink">{label}</span>
      <div className="flex items-center gap-1.5 shrink-0">
        <input type="number" value={value} min={min} max={max} onChange={e => onChange(Number(e.target.value))}
          className="input-field !w-24 !py-1 !text-xs text-right" />
        {unit && <span className="text-xs text-white/30 w-8">{unit}</span>}
      </div>
    </div>
  );
}

function FloatInput({ label, value, onChange, step }: {
  label: string; value: number; onChange: (v: number) => void; step?: number;
}) {
  return (
    <div className="flex items-center justify-between py-2 gap-3 min-w-0">
      <span className="text-sm text-white/70 truncate min-w-0 shrink">{label}</span>
      <input type="number" value={value} step={step ?? 0.01} onChange={e => onChange(Number(e.target.value))}
        className="input-field !w-24 !py-1 !text-xs text-right shrink-0" />
    </div>
  );
}

const DEFAULT_AGENT: AgentLimitsConfig = { max_tool_rounds: 10, default_tool_timeout: 120, max_history_messages: 100 };
const DEFAULT_SECURITY: SecurityCommandsConfig = { block_high_risk: true, require_medium_approval: false, extra_read_only: [], extra_destructive: [], extra_dangerous_env: [], extra_allowed_commands: [] };
const DEFAULT_SHELL: ShellConfig = { max_output_bytes: 1048576, safe_env_vars: [] };
const DEFAULT_BREAKER: CircuitBreakerConfig = { max_repeat_failures: 3, max_no_progress_fails: 6, max_hard_rejects: 2 };
const DEFAULT_PIPELINE: MemoryPipelineConfig = { worker_count: 2, tree_bucket_size: 10, archive_msg_limit: 200, freshness_half_life: 168 };
const DEFAULT_WEIGHTS: RetrievalWeightsConfig = { fts5: 0.25, vector: 0.20, keyword: 0.08, tree: 0.12, graph: 0.20, episodic: 0.15 };
const DEFAULT_COST: CostConfig = { budget_cents: 10000 };

export function ConfigSettings() {
  const { t } = useT();
  const [section, setSection] = useState<Section>('agent');
  const [agentLimits, setAgentLimits] = useState<AgentLimitsConfig>(DEFAULT_AGENT);
  const [securityCmds, setSecurityCmds] = useState<SecurityCommandsConfig>(DEFAULT_SECURITY);
  const [shell, setShell] = useState<ShellConfig>(DEFAULT_SHELL);
  const [breaker, setBreaker] = useState<CircuitBreakerConfig>(DEFAULT_BREAKER);
  const [memPipeline, setMemPipeline] = useState<MemoryPipelineConfig>(DEFAULT_PIPELINE);
  const [retrievalWeights, setRetrievalWeights] = useState<RetrievalWeightsConfig>(DEFAULT_WEIGHTS);
  const [cost, setCost] = useState<CostConfig>(DEFAULT_COST);
  const [dataDir, setDataDir] = useState('');
  const [saving, setSaving] = useState(false);
  const [saveMsg, setSaveMsg] = useState('');

  const showMsg = (msg: string) => { setSaveMsg(msg); setTimeout(() => setSaveMsg(''), 3000); };

  useEffect(() => {
    api.getAgentLimits().then(setAgentLimits).catch(() => {});
    api.getSecurityCommands().then(setSecurityCmds).catch(() => {});
    api.getShellConfig().then(setShell).catch(() => {});
    api.getCircuitBreakerConfig().then(setBreaker).catch(() => {});
    api.getMemoryPipelineConfig().then(setMemPipeline).catch(() => {});
    api.getRetrievalWeights().then(setRetrievalWeights).catch(() => {});
    api.getCostConfig().then(setCost).catch(() => {});
    api.getWorkspace().then(setDataDir).catch(() => {});
  }, []);

  const save = async (fn: () => Promise<void>) => {
    setSaving(true);
    try { await fn(); showMsg(t('settings.save')); } catch (e) { showMsg(`${t('errors.somethingWrong')}: ${e}`); }
    finally { setSaving(false); }
  };

  return (
    <div className="animate-fade-in">
      <div className="flex gap-2 mb-6 flex-wrap">
        {SECTIONS.map(s => (
          <button key={s.id} onClick={() => setSection(s.id)}
            className={`px-3 py-1.5 rounded-lg text-xs transition-all ${
              section === s.id ? 'bg-ocean-500/15 text-ocean-300' : 'text-white/40 hover:text-white/70 hover:bg-white/5'
            }`}>
            {t(s.key)}
          </button>
        ))}
      </div>

      {saveMsg && <div className="mb-4 text-xs text-sage-400 animate-fade-in">{saveMsg}</div>}

      <div className="max-w-lg">
        {/* Agent Limits */}
        {section === 'agent' && agentLimits && (
          <div className="card space-y-1">
            <SectionHeader title={t('settings.agentLimits')} desc={t('settings.agentLimitsDesc')} />
            <NumberInput label={t('settings.maxToolRounds')} value={agentLimits.max_tool_rounds}
              onChange={v => setAgentLimits({ ...agentLimits, max_tool_rounds: v })} min={2} max={50} />
            <NumberInput label={t('settings.toolTimeout')} value={agentLimits.default_tool_timeout}
              onChange={v => setAgentLimits({ ...agentLimits, default_tool_timeout: v })} min={10} max={600} unit="s" />
            <NumberInput label={t('settings.maxHistoryMessages')} value={agentLimits.max_history_messages}
              onChange={v => setAgentLimits({ ...agentLimits, max_history_messages: v })} min={10} max={500} />
            <button className="btn-primary text-xs mt-4" disabled={saving}
              onClick={() => save(() => api.setAgentLimits(agentLimits))}>{t('settings.save')}</button>
          </div>
        )}

        {/* Security Commands */}
        {section === 'security' && securityCmds && (
          <div className="card space-y-1">
            <SectionHeader title={t('settings.security')} desc={t('settings.securityDesc')} />
            <Toggle label={t('settings.blockHighRisk')} value={securityCmds.block_high_risk}
              onChange={v => setSecurityCmds({ ...securityCmds, block_high_risk: v })}
              desc={t('settings.blockHighRiskDesc')} />
            <Toggle label={t('settings.requireMediumApproval')} value={securityCmds.require_medium_approval}
              onChange={v => setSecurityCmds({ ...securityCmds, require_medium_approval: v })}
              desc={t('settings.requireMediumApprovalDesc')} />
            <div className="py-2">
              <label className="text-xs text-white/50 mb-1 block">额外允许的命令（逗号分隔，supervised等级下生效）</label>
              <input className="input-field text-xs" type="text"
                value={securityCmds.extra_allowed_commands?.join(', ') || ''}
                onChange={e => setSecurityCmds({ ...securityCmds, extra_allowed_commands: e.target.value.split(',').map(s => s.trim()).filter(Boolean) })}
                placeholder="如: python3, docker, make" />
            </div>
            <button className="btn-primary text-xs mt-4" disabled={saving}
              onClick={() => save(() => api.setSecurityCommands(securityCmds))}>{t('settings.save')}</button>
          </div>
        )}

        {/* Shell Tool */}
        {section === 'shell' && shell && (
          <div className="card space-y-1">
            <SectionHeader title={t('settings.shellTool')} desc={t('settings.shellToolDesc')} />
            <NumberInput label={t('settings.maxOutputBytes')} value={shell.max_output_bytes}
              onChange={v => setShell({ ...shell, max_output_bytes: v })} min={65536} max={104857600} unit="B" />
            <button className="btn-primary text-xs mt-4" disabled={saving}
              onClick={() => save(() => api.setShellConfig(shell))}>{t('settings.save')}</button>
          </div>
        )}

        {/* Memory Pipeline */}
        {section === 'memory' && memPipeline && retrievalWeights && (
          <div className="space-y-4">
            <div className="card space-y-1">
              <SectionHeader title={t('settings.memoryPipeline')} desc={t('settings.memoryPipelineDesc')} />
              <NumberInput label={t('settings.workerCount')} value={memPipeline.worker_count}
                onChange={v => setMemPipeline({ ...memPipeline, worker_count: v })} min={1} max={8} />
              <NumberInput label={t('settings.treeBucketSize')} value={memPipeline.tree_bucket_size}
                onChange={v => setMemPipeline({ ...memPipeline, tree_bucket_size: v })} min={2} max={50} />
              <NumberInput label={t('settings.archiveMsgLimit')} value={memPipeline.archive_msg_limit}
                onChange={v => setMemPipeline({ ...memPipeline, archive_msg_limit: v })} min={10} max={1000} />
              <FloatInput label={t('settings.freshnessHalfLife')} value={memPipeline.freshness_half_life}
                onChange={v => setMemPipeline({ ...memPipeline, freshness_half_life: v })} step={1} />
              <button className="btn-primary text-xs mt-4" disabled={saving}
                onClick={() => save(() => api.setMemoryPipelineConfig(memPipeline))}>{t('settings.save')}</button>
            </div>
            <div className="card space-y-1">
              <SectionHeader title={t('settings.retrievalWeights')} desc={t('settings.retrievalWeightsDesc')} />
              {WEIGHT_KEYS.map(k => (
                <FloatInput key={k} label={k} value={retrievalWeights[k]}
                  onChange={v => setRetrievalWeights({ ...retrievalWeights, [k]: v })} />
              ))}
              <div className="text-xs text-white/20 mt-1">
                {t('settings.sum')}: {WEIGHT_KEYS.reduce((a, k) => a + retrievalWeights[k], 0).toFixed(2)}
              </div>
              <button className="btn-primary text-xs mt-4" disabled={saving}
                onClick={() => save(() => api.setRetrievalWeights(retrievalWeights))}>{t('settings.save')}</button>
            </div>
          </div>
        )}

        {/* Circuit Breaker */}
        {section === 'breaker' && breaker && (
          <div className="card space-y-1">
            <SectionHeader title={t('settings.circuitBreaker')} desc={t('settings.circuitBreakerDesc')} />
            <NumberInput label={t('settings.repeatFailures')} value={breaker.max_repeat_failures}
              onChange={v => setBreaker({ ...breaker, max_repeat_failures: v })} min={1} max={10} />
            <NumberInput label={t('settings.noProgressFails')} value={breaker.max_no_progress_fails}
              onChange={v => setBreaker({ ...breaker, max_no_progress_fails: v })} min={2} max={20} />
            <NumberInput label={t('settings.hardRejects')} value={breaker.max_hard_rejects}
              onChange={v => setBreaker({ ...breaker, max_hard_rejects: v })} min={1} max={5} />
            <button className="btn-primary text-xs mt-4" disabled={saving}
              onClick={() => save(() => api.setCircuitBreakerConfig(breaker))}>{t('settings.save')}</button>
          </div>
        )}

        {/* Cost */}
        {section === 'cost' && cost && (
          <div className="card space-y-1">
            <SectionHeader title={t('settings.costBudget')} desc={t('settings.costBudgetDesc')} />
            <FloatInput label={t('settings.monthlyBudget')} value={cost.budget_cents}
              onChange={v => setCost({ ...cost, budget_cents: Math.round(v) })} step={100} />
            <p className="text-xs text-white/25">${(cost.budget_cents / 100).toFixed(2)}</p>
            <button className="btn-primary text-xs mt-4" disabled={saving}
              onClick={() => save(() => api.setCostConfig(cost))}>{t('settings.save')}</button>
          </div>
        )}

        {section === 'paths' && (
          <div className="card space-y-1">
            <SectionHeader title={t('settings.dataDir')} desc={t('settings.dataDirDesc')} />
            <div>
              <input value={dataDir} onChange={e => setDataDir(e.target.value)}
                className="input-field !py-1.5 !text-xs !font-mono" placeholder={t('settings.dataDirPlaceholder')} />
              <p className="text-xs text-white/20 mt-1">{t('settings.dataDirHint')}</p>
            </div>
            <button className="btn-primary text-xs mt-4" disabled={saving}
              onClick={() => save(() => api.setWorkspace(dataDir))}>{t('settings.save')}</button>
          </div>
        )}
      </div>
    </div>
  );
}
