// Typed wrappers for Go backend Wails bindings.
// Methods are exposed on window['go']['main']['App'] by the Wails runtime.

const DEFAULT_TIMEOUT_MS = 30_000;  // 30 seconds
const MAX_RETRIES = 2;

export class RpcError extends Error {
  constructor(message: string, public code?: string, public status?: number) {
    super(message);
    this.name = 'RpcError';
  }
}

function invoke<T>(method: string, ...args: unknown[]): Promise<T> {
  const w = window as any;
  const configRpcMethods = ['ListProviders', 'AddProvider', 'UpdateProvider', 'RemoveProvider',
    'TestProviderConnection', 'GetWorkspace', 'SetWorkspace',
    'GetExtensionConfig', 'SetExtensionConfig',
    'GetAgentLimits', 'SetAgentLimits', 'GetSecurityCommands', 'SetSecurityCommands',
    'SetSecurityTier', 'GetShellConfig', 'SetShellConfig',
    'GetCircuitBreakerConfig', 'SetCircuitBreakerConfig',
    'GetMemoryPipelineConfig', 'SetMemoryPipelineConfig',
    'GetRetrievalWeights', 'SetRetrievalWeights',
    'GetCostConfig', 'SetCostConfig',
    'GetPrompt', 'GetDefaultPrompt', 'SetPrompt', 'DeletePrompt', 'ListPrompts',
    'ListModels', 'SetDefaultModel', 'GetModelRoutes', 'SetModelRoute',
    'GetVoiceConfig', 'SetVoiceConfig',
  ];
  const aboutMethods = ['GetCurrentVersion', 'CheckForUpdate'];
  const costMethods = ['CostDashboard', 'GetCostOverview'];
  const cronMethods = ['GetCronJobs', 'ToggleCronJob', 'TriggerCronJob', 'AddCronJob', 'RemoveCronJob'];
  const learningMethods = ['GetPreferences'];
  const memoryMethods = ['SearchMemory', 'ListNamespaces', 'ClearNamespace', 'SetRetrievalProfile'];
  const companionMethods = ['ActivateCompanion', 'StartCompanionLoop', 'StopCompanionLoop', 'StartScreenIntel', 'StopScreenIntel', 'StartScreenIntelligence', 'GetVoiceEngines', 'RegisterActivateCallback'];
  const threadMethods = ['ListThreads', 'CreateThread', 'DeleteThread', 'GetThreadMessages', 'UpdateThreadTitle'];
  const webhookMethods = ['ListTunnels', 'CreateTunnel', 'GetTunnel', 'UpdateTunnel',
    'DeleteTunnel', 'GetTunnelBandwidth', 'ListTunnelActivity', 'ClearTunnelActivity'];
  const agentMethods = ['ListAgents', 'UpsertAgent', 'RemoveAgent'];
  const approvalMethods = ['ListPendingApprovals', 'DecideApproval', 'ListApprovalAllowlist'];
  const keyringMethods = ['KeyringStatus', 'KeyringRetryProbe', 'KeyringConsentDecide'];
  const healthMethods = ['DoctorReport'];
  const appStateMethods = ['AppStateSnapshot'];
  const mcpMethods = ['ListMCPServers', 'InstallMCPServer', 'UninstallMCPServer', 'GetMCPServerStatus', 'CallMCPTool'];
	const capabilityMethods = ['ListSets', 'GetSet', 'ListAllTools', 'GetToolSchema', 'GetToolDiagnostics', 'ListAllAgents',
		'AddMCPServer', 'RemoveMCPServer', 'ConnectMCPServer', 'DisconnectMCPServer',
		'InstallLegacyExtension', 'InstallSkill', 'ListSkillCatalog', 'RefreshSkillCatalog', 'InstallSkillFromCatalog',
		'ListExtensions', 'InstallExtension', 'UninstallExtension'];
  const soulMethods = ["GetSOUL", "SetSOUL", "GetIdentity", "SetIdentity"];
  const channelMethods = ['ListChannels', 'EnableChannel', 'DisableChannel'];
  const monitorMethods = ['ListRuns', 'StartRun', 'GetRun', 'ReadOutput', 'StopRun'];
  const service = soulMethods.includes(method) ? "SoulRPC"
    : channelMethods.includes(method) ? "ChannelRPC"
    : configRpcMethods.includes(method) ? "ConfigRPC"
    : keyringMethods.includes(method) ? 'KeyringRPC'
    : approvalMethods.includes(method) ? 'ApprovalRPC'
    : agentMethods.includes(method) ? 'AgentRPC'
    : aboutMethods.includes(method) ? 'AboutRPC'
    : costMethods.includes(method) ? 'CostRPC'
    : cronMethods.includes(method) ? 'CronRPC'
    : learningMethods.includes(method) ? 'LearningRPC'
    : memoryMethods.includes(method) ? 'MemoryRPC'
    : companionMethods.includes(method) ? 'DesktopRPC'
    : webhookMethods.includes(method) ? 'WebhookRPC'
    : threadMethods.includes(method) ? 'ThreadsRPC'
    : healthMethods.includes(method) ? 'HealthRPC'
    : appStateMethods.includes(method) ? 'AppStateRPC'
    : mcpMethods.includes(method) ? 'RPC'
	    : capabilityMethods.includes(method) ? 'CapabilityRPC'
    : monitorMethods.includes(method) ? 'MonitorRPC'
    : 'App';

  // Wails binds structs under go.<package>.<StructName>. Try the main
  // package path first (for structs defined in main.go), then fall back
  // to the struct's own package (e.g. config.ConfigRPC → go.config.ConfigRPC).
  const goMain = w['go']?.['main'];
  let fn = goMain?.[service]?.[method];
  if (!fn) {
    // Map service name → {package, struct}. If struct is empty, the
    // service name itself is the Wails struct name (default).
    const svcMap: Record<string, {pkg: string; name?: string}> = {
      'ConfigRPC':     {pkg: 'config'},
      'ApprovalRPC':   {pkg: 'approval'},
      'AgentRPC':      {pkg: 'agent'},
      'ToolsRPC':      {pkg: 'tools'},
      'CostRPC':       {pkg: 'cost'},
      'CronRPC':       {pkg: 'cron'},
      'LearningRPC':   {pkg: 'learning'},
      'MemoryRPC':     {pkg: 'memory'},
      'DesktopRPC':    {pkg: 'desktop'},
      'KeyringRPC':    {pkg: 'keyring'},
      'WebhookRPC':    {pkg: 'webhooks'},
      'ThreadsRPC':    {pkg: 'conversations'},
      'HealthRPC':     {pkg: 'health'},
      'AppStateRPC':   {pkg: 'app_state'},
      'AboutRPC':      {pkg: 'about',   name: 'RPC'},
      'RPC':           {pkg: 'registry', name: 'RPC'},
            'SoulRPC':      {pkg: 'soul', name: 'RPC'},
      'MonitorRPC':    {pkg: 'monitor', name: 'RPC'},
      'CapabilityRPC': {pkg: 'capability'},
      'ChannelRPC':    {pkg: 'channels', name: 'ChannelRPC'},
    };
    const entry = svcMap[service];
    if (entry) {
      const structName = entry.name || service;
      fn = w['go']?.[entry.pkg]?.[structName]?.[method];
      if (fn) {
        console.debug(`Wails: ${method} found at go.${entry.pkg}.${structName} (fallback)`);
      }
    }
  }
  if (!fn) {
    console.debug(`Wails method ${method} not available — running in browser mode.`);
    return Promise.reject(new RpcError(`Wails method ${method} not available`, 'NOT_AVAILABLE'));
  }
  return fn(...args) as Promise<T>;
}

// invokeWithTimeout wraps invoke with a configurable timeout.
export async function invokeWithTimeout<T>(
  method: string,
  args: unknown[],
  timeoutMs: number = DEFAULT_TIMEOUT_MS,
): Promise<T> {
  const timer = new Promise<never>((_, reject) =>
    setTimeout(() => reject(new RpcError(`RPC ${method} timed out after ${timeoutMs}ms`, 'TIMEOUT')), timeoutMs)
  );
  try {
    return await Promise.race([invoke<T>(method, ...args), timer]);
  } catch (e) {
    if (e instanceof RpcError) throw e;
    throw new RpcError(
      e instanceof Error ? e.message : String(e),
      'RPC_ERROR',
    );
  }
}

// invokeWithRetry retries transient RPC failures up to MAX_RETRIES times.
export async function invokeWithRetry<T>(
  method: string,
  args: unknown[],
  timeoutMs: number = DEFAULT_TIMEOUT_MS,
): Promise<T> {
  let lastError: Error | undefined;
  for (let attempt = 0; attempt <= MAX_RETRIES; attempt++) {
    try {
      return await invokeWithTimeout<T>(method, args, timeoutMs);
    } catch (e) {
      lastError = e instanceof Error ? e : new Error(String(e));
      if (attempt < MAX_RETRIES && isTransientError(lastError)) {
        await new Promise(r => setTimeout(r, Math.min(1000 * (attempt + 1), 5000)));
        continue;
      }
      throw lastError;
    }
  }
  throw lastError!;
}

// isTransientError returns true for errors likely to resolve on retry.
function isTransientError(err: Error): boolean {
  const msg = err.message.toLowerCase();
  const transient = ['timeout', 'network', 'econnrefused', 'econnreset', '503', '502', '429', 'temporarily'];
  return transient.some(t => msg.includes(t));
}

export class AbortError extends RpcError {
  constructor(method: string) {
    super(`RPC ${method} was aborted`, 'ABORTED');
    this.name = 'AbortError';
  }
}

// invokeWithAbort wraps invoke with AbortController support.
// Pass an AbortSignal to cancel in-flight requests.
export async function invokeWithAbort<T>(
  method: string,
  args: unknown[],
  signal?: AbortSignal,
  timeoutMs: number = DEFAULT_TIMEOUT_MS,
): Promise<T> {
  if (signal?.aborted) {
    throw new AbortError(method);
  }

  const timer = new Promise<never>((_, reject) => {
    const id = setTimeout(() => reject(new RpcError(`RPC ${method} timed out after ${timeoutMs}ms`, 'TIMEOUT')), timeoutMs);
    if (signal) {
      signal.addEventListener('abort', () => {
        clearTimeout(id);
        reject(new AbortError(method));
      }, { once: true });
    }
  });

  try {
    return await Promise.race([invoke<T>(method, ...args), timer]);
  } catch (e) {
    if (e instanceof RpcError) throw e;
    throw new RpcError(e instanceof Error ? e.message : String(e), 'RPC_ERROR');
  }
}

// invokeWithRetryAndAbort combines retry logic with abort support.
export async function invokeWithRetryAndAbort<T>(
  method: string,
  args: unknown[],
  signal?: AbortSignal,
  timeoutMs: number = DEFAULT_TIMEOUT_MS,
): Promise<T> {
  let lastError: Error | undefined;
  for (let attempt = 0; attempt <= MAX_RETRIES; attempt++) {
    if (signal?.aborted) throw new AbortError(method);
    try {
      return await invokeWithAbort<T>(method, args, signal, timeoutMs);
    } catch (e) {
      lastError = e instanceof Error ? e : new Error(String(e));
      if (e instanceof AbortError) throw e;
      if (attempt < MAX_RETRIES && isTransientError(lastError)) {
        await new Promise(r => setTimeout(r, Math.min(1000 * (attempt + 1), 5000)));
        continue;
      }
      throw lastError;
    }
  }
  throw lastError!;
}

// ── Chat ──────────────────────────────────────────────────────────────

export interface ChatRequest {
  threadId: string;
  message: string;
}
export interface ChatResponse {
  threadId: string;
  content: string;
  done: boolean;
}
// sendMessage sends a chat message and waits for the full response.
// Supports optional AbortSignal for cancellation. Uses retry on transient errors.
export function sendMessage(req: ChatRequest, signal?: AbortSignal) {
  return invokeWithRetryAndAbort<ChatResponse>('SendMessage', [req], signal, 120_000);
}

// streamChatMessage initiates a streaming chat turn. Tokens arrive via Wails
// events: "chat:token", "chat:tool_call", "chat:tool_result", "chat:done", "chat:error".
// Uses a 120s timeout for long-running agent turns.
export function streamChatMessage(req: ChatRequest) {
  return invokeWithTimeout<void>('StreamChatMessage', [req], 120_000);
}

// Event types for streaming chat listeners.
// Mirrors the Go event bus kinds (pkg/events/bus.go) bridged via Wails events.
export type ChatStreamEventType =
  // Core streaming
  | 'token' | 'text_delta' | 'thinking_delta' | 'tool_args_delta'
  | 'tool_call' | 'tool_result' | 'done' | 'error'
  // Turn lifecycle
  | 'inference_start' | 'iteration_start' | 'iteration_end'
  | 'turn_started' | 'turn_completed'
  // Sub-agent events
  | 'subagent_spawned' | 'subagent_completed' | 'subagent_failed'
  // Artifact events
  | 'artifact_started' | 'artifact_progress' | 'artifact_ready' | 'artifact_failed'
  // Orchestration
  | 'orchestration_step' | 'awaiting_user'
  | 'compaction_triggered' | 'session_expired'
  // Task board
  | 'task_board_updated';

export interface ChatStreamEvent {
  threadId: string;
  content: string;
  done: boolean;
  type: ChatStreamEventType;
  // Extended fields for richer event types.
  toolName?: string;
  toolArgs?: Record<string, unknown>;
  toolCallId?: string;
  artifactId?: string;
  subagentName?: string;
  subagentId?: string;
  iterationIndex?: number;
  errorCode?: string;
  taskBoard?: Record<string, unknown>;
}

// ── Event bus types ────────────────────────────────────────────────────

export type DomainEventKind =
  // Agent domain
  | 'agent.turn_started' | 'agent.turn_completed' | 'agent.error'
  | 'agent.subagent_spawned' | 'agent.subagent_completed' | 'agent.subagent_failed'
  | 'agent.orchestration_step' | 'agent.awaiting_user'
  | 'agent.compaction_triggered' | 'agent.session_expired'
  // Memory domain
  | 'memory.stored' | 'memory.recalled'
  | 'memory.ingestion_started' | 'memory.ingestion_completed'
  | 'memory.sync_started' | 'memory.sync_completed' | 'memory.sync_failed'
  | 'memory.archive_completed' | 'memory.tree_rebuilt'
  | 'memory.entity_extracted' | 'memory.graph_updated'
  | 'memory.document_canonicalized'
  // Channel domain
  | 'channel.inbound_message' | 'channel.connected'
  | 'channel.disconnected' | 'channel.reaction'
  // Cron domain
  | 'cron.triggered'
  // Approval domain
  | 'approval.requested' | 'approval.decided'
  // Tool domain
  | 'tool.execution_started' | 'tool.execution_completed' | 'tool.execution_failed'
  // System domain
  | 'system.startup' | 'system.shutdown' | 'system.health';

export interface DomainEvent {
  kind: DomainEventKind;
  timestamp: string;
  data?: Record<string, unknown>;
}

// ── Threads ───────────────────────────────────────────────────────────

export interface ThreadSummary {
  id: string;
  title: string;
  created_at: string;
  updated_at: string;
  message_count: number;
}
export interface MessageRecord {
  id: number;
  role: string;
  content: string;
  created_at: string;
}
export function listThreads() { return invoke<ThreadSummary[]>('ListThreads'); }
export function createThread(title: string) { return invoke<ThreadSummary>('CreateThread', title); }
export function updateThreadTitle(threadId: string, title: string) { return invoke<void>('UpdateThreadTitle', threadId, title); }
export function deleteThread(threadId: string) { return invoke<void>('DeleteThread', threadId); }
export function getThreadMessages(threadId: string, limit?: number, afterId?: number) {
  return invoke<MessageRecord[]>('GetThreadMessages', threadId, limit ?? 200, afterId ?? 0);
 }

// ── Todo board ────────────────────────────────────────────────────────

export interface TodoSnapshot {
  thread_id: string;
  cards: TodoCard[];
}
export interface TodoCard {
  id: string;
  title: string;
  notes: string;
  status: string;
  created_at: string;
}
export function listTodos(threadId: string) { return invoke<TodoSnapshot>('ListTodos', threadId); }
export function addTodo(threadId: string, title: string, notes: string) { return invoke<TodoSnapshot>('AddTodo', threadId, title, notes); }
export function updateTodoStatus(threadId: string, cardId: string, status: string) { return invoke<TodoSnapshot>('UpdateTodoStatus', threadId, cardId, status); }
export function removeTodo(threadId: string, cardId: string) { return invoke<TodoSnapshot>('RemoveTodo', threadId, cardId); }

// ── Approval ──────────────────────────────────────────────────────────

export interface PendingApproval {
  id: string;
  tool_name: string;
  args: string;
  reason: string;
  created_at: string;
  expires_at: string;
}
export interface AllowlistEntry {
  tool_name: string;
  created_at: string;
}
export function listPendingApprovals() { return invoke<PendingApproval[]>('ListPendingApprovals'); }
export function decideApproval(id: string, decision: 'approve_once' | 'approve_always' | 'deny') {
  return invoke<{ ok: boolean }>('DecideApproval', id, decision);
}
export function listApprovalAllowlist() { return invoke<AllowlistEntry[]>('ListApprovalAllowlist'); }

// ── Agent ─────────────────────────────────────────────────────────────

export interface AgentInfo {
  id: string;
  name: string;
  description: string;
  systemPrompt?: string;
  tier?: string;
  model?: string;
  toolAllowlist?: string[];
  toolDenylist?: string[];
  subagents?: { agent_id: string; skills_filter: string; delegate_name?: string }[];
  maxIterations?: number;
  hidden?: boolean;
  forkMode?: boolean;
  timeoutSecs?: number;
  sandboxMode?: string;
}
export function listAgents() { return invoke<AgentInfo[]>('ListAgents'); }
export function upsertAgent(id: string, name: string, description: string, systemPrompt: string) {
  return invoke<void>('UpsertAgent', id, name, description, systemPrompt);
}
export function removeAgent(id: string) { return invoke<void>('RemoveAgent', id); }

// ── Tools ──────────────────────────────────────────────────────────────

export interface ToolInfo {
  name: string;
  description: string;
  parameters: unknown;
}
export function listTools() { return invoke<ToolInfo[]>('ListTools'); }

// ── Memory ────────────────────────────────────────────────────────────

export function searchMemory(query: string, filter?: string) { return invoke<string>('SearchMemory', query, filter || ''); }

// ── Keyring ──────────────────────────────────────────────────────────

export function keyringStatus() { return invoke<Record<string, unknown>>('KeyringStatus'); }
export function keyringConsentDecide(mode: string) { return invoke<Record<string, unknown>>('KeyringConsentDecide', mode); }
export function keyringRetryProbe() { return invoke<Record<string, unknown>>('KeyringRetryProbe'); }

// ── Provider Connection Test ──────────────────────────────────────────

export interface ProviderTestResult {
  ok: boolean;
  provider?: string;
  endpoint?: string;
  status?: number;
  error?: string;
  warning?: string;
}
export function testProviderConnection(providerName: string) {
  return invoke<ProviderTestResult>('TestProviderConnection', providerName);
}

// ── Diagnostics ──────────────────────────────────────────────────────

export interface ToolDiagnostics {
  ok: boolean;
  totalTools: number;
  enabledTools: number;
  mcpStdioTools: number;
  jsonRpcTools: number;
  inProcessTools: number;
  writeSurfaces: number;
  bySource: Record<string, number>;
  recentDenials: Array<{ tool_name: string; reason: string; args_digest: string; time: string }>;
}
export function getToolDiagnostics() { return invoke<ToolDiagnostics>('GetToolDiagnostics'); }

export interface DoctorCheck {
  name: string;
  status: string;
  message: string;
}
export interface DoctorReport {
  ok: boolean;
  workspace: string;
  db_healthy: boolean;
  checks: DoctorCheck[];
  startup_errors?: string[];
  status?: string;
}
export function getDoctorReport() { return invoke<DoctorReport>('DoctorReport'); }

export interface CostDashboard {
  ok: boolean;
  overview: string;
  total_cost_cents: number;
  budget_used_pct: number;
}
export function getCostDashboard() { return invoke<CostDashboard>('CostDashboard'); }

export interface AppStateSnapshot {
  ok: boolean;
  tool_count: number;
  agent_count: number;
  pending_approvals: number;
  provider_ready: boolean;
  db_ready: boolean;
}
export function getAppStateSnapshot() { return invoke<AppStateSnapshot>('AppStateSnapshot'); }

// ── Dashboard / Monitoring ───────────────────────────────────────────

export function health() { return invoke<Record<string, unknown>>('Health'); }
export function getCostOverview() { return invoke<string>('GetCostOverview'); }
export function getCronJobs() { return invoke<unknown[]>('GetCronJobs'); }
export function toggleCronJob(id: string, enabled: boolean) { return invoke<{ ok: boolean; error?: string }>('ToggleCronJob', id, enabled); }
export function triggerCronJob(id: string) { return invoke<{ ok: boolean; error?: string }>('TriggerCronJob', id); }
export function addCronJob(name: string, schedule: string, prompt: string) { return invoke<{ ok: boolean; id?: string; error?: string }>('AddCronJob', name, schedule, prompt); }
export function removeCronJob(id: string) { return invoke<{ ok: boolean; error?: string }>('RemoveCronJob', id); }
export function getPreferences() { return invoke<unknown[]>('GetPreferences'); }
export function getSubconsciousStats() { return invoke<Record<string, unknown>>('GetSubconsciousStats'); }
export function getReflections(limit: number) { return invoke<unknown[]>('GetReflections', limit).catch(() => []); }

// ── Monitor ─────────────────────────────────────────────────────────

export interface MonitorRunSummary {
  id: string;
  command: string;
  status: string;
  exit_code: number;
  error?: string;
  started_at: number;
  ended_at?: number;
}
export interface MonitorRunList { runs: MonitorRunSummary[]; total: number; }
export function listMonitorRuns() { return invoke<MonitorRunList>('ListRuns'); }
export function startMonitorRun(command: string, timeoutSecs: number) {
  return invoke<{ run_id: string }>('StartRun', { command, timeout_secs: timeoutSecs });
}
export function getMonitorRun(runID: string) { return invoke<MonitorRunSummary>('GetRun', runID); }
export function readMonitorOutput(runID: string) { return invoke<string>('ReadOutput', runID); }
export function stopMonitorRun(runID: string) { return invoke<void>('StopRun', runID); }

// ── Voice ───────────────────────────────────────────────────────────────

export interface VoiceConfig {
  stt_provider: string;
  stt_model: string;
  stt_endpoint: string;
  stt_api_key: string;
  tts_provider: string;
  tts_model: string;
  tts_endpoint: string;
  tts_api_key: string;
}
export function getVoiceConfig() { return invoke<VoiceConfig>('GetVoiceConfig'); }
export function setVoiceConfig(cfg: Partial<VoiceConfig>) { return invoke<void>('SetVoiceConfig', cfg); }

// ── Companion ─────────────────────────────────────────────────────────

export function activateCompanion() { return invoke<string>('ActivateCompanion'); }
export function startCompanionLoop() { return invoke<string>('StartCompanionLoop'); }
export function stopCompanionLoop() { return invoke<void>('StopCompanionLoop'); }
export function startScreenIntel() { return invoke<string>('StartScreenIntel'); }
export function stopScreenIntel() { return invoke<string>('StopScreenIntel'); }
export function startScreenIntelligence(intervalSecs: number) { return invoke<string>('StartScreenIntelligence', intervalSecs); }
export function getVoiceEngines() { return invoke<{ stt: string; tts: string }>('GetVoiceEngines'); }
export function registerActivateCallback() { return invoke<void>('RegisterActivateCallback'); }

// ── Updates ───────────────────────────────────────────────────────────

export function checkForUpdate() { return invoke<unknown>('CheckForUpdate'); }
export function getCurrentVersion() { return invoke<string>('GetCurrentVersion'); }

// ── Config ──────────────────────────────────────────────────────────────

export interface AgentLimitsConfig {
  max_tool_rounds: number;
  default_tool_timeout: number;
  max_history_messages: number;
}
export interface SecurityCommandsConfig {
  block_high_risk: boolean;
  require_medium_approval: boolean;
  extra_read_only: string[];
  extra_destructive: string[];
  extra_dangerous_env: string[];
  extra_allowed_commands: string[];
}
export interface ShellConfig {
  max_output_bytes: number;
  safe_env_vars: string[];
}
export interface CircuitBreakerConfig {
  max_repeat_failures: number;
  max_no_progress_fails: number;
  max_hard_rejects: number;
}
export interface MemoryPipelineConfig {
  worker_count: number;
  tree_bucket_size: number;
  archive_msg_limit: number;
  freshness_half_life: number;
}
export interface RetrievalWeightsConfig {
  fts5: number; vector: number; keyword: number;
  tree: number; graph: number; episodic: number;
}
export interface CostConfig { budget_cents: number; }

export function getAgentLimits() { return invoke<AgentLimitsConfig>('GetAgentLimits'); }
export function setAgentLimits(l: AgentLimitsConfig) { return invoke<void>('SetAgentLimits', l); }
export function getSecurityCommands() { return invoke<SecurityCommandsConfig>('GetSecurityCommands'); }
export function setSecurityCommands(c: SecurityCommandsConfig) { return invoke<void>('SetSecurityCommands', c); }
export function setSecurityTier(tier: string) { return invoke<void>('SetSecurityTier', tier); }
export function getShellConfig() { return invoke<ShellConfig>('GetShellConfig'); }
export function setShellConfig(c: ShellConfig) { return invoke<void>('SetShellConfig', c); }
export function getCircuitBreakerConfig() { return invoke<CircuitBreakerConfig>('GetCircuitBreakerConfig'); }
export function setCircuitBreakerConfig(c: CircuitBreakerConfig) { return invoke<void>('SetCircuitBreakerConfig', c); }
export function getMemoryPipelineConfig() { return invoke<MemoryPipelineConfig>('GetMemoryPipelineConfig'); }
export function setMemoryPipelineConfig(c: MemoryPipelineConfig) { return invoke<void>('SetMemoryPipelineConfig', c); }
export function getRetrievalWeights() { return invoke<RetrievalWeightsConfig>('GetRetrievalWeights'); }
export function setRetrievalWeights(w: RetrievalWeightsConfig) { return invoke<void>('SetRetrievalWeights', w); }
export function getCostConfig() { return invoke<CostConfig>('GetCostConfig'); }
export function setCostConfig(c: CostConfig) { return invoke<void>('SetCostConfig', c); }

// ── Prompts ────────────────────────────────────────────────────────────

export interface PromptMeta {
  name: string;
  description: string;
  length: number;
  overridden: boolean;
  default_length: number;
  builtin: boolean;
}
export function getPrompt(name: string) { return invoke<string>('GetPrompt', name); }
export function getDefaultPrompt(name: string) { return invoke<string>('GetDefaultPrompt', name); }
export function setPrompt(name: string, body: string) { return invoke<void>('SetPrompt', name, body); }
export function deletePrompt(name: string) { return invoke<void>('DeletePrompt', name); }
export function listPrompts() { return invoke<PromptMeta[]>('ListPrompts'); }

// ── Providers ──────────────────────────────────────────────────────────

export interface ProviderConfig {
  name: string;
  type: string;
  api_key: string;
  base_url: string;
  models: string[];
}
export function listProviders() { return invoke<ProviderConfig[]>('ListProviders'); }
export function addProvider(p: ProviderConfig) { return invoke<void>('AddProvider', p); }
export function updateProvider(name: string, p: ProviderConfig) { return invoke<void>('UpdateProvider', name, p); }
export function removeProvider(name: string) { return invoke<void>('RemoveProvider', name); }
export function setDefaultModel(model: string) { return invoke<void>('SetDefaultModel', model); }
export function getModelRoutes() { return invoke<Record<string, string>>('GetModelRoutes'); }
export function setModelRoute(kind: string, model: string) { return invoke<void>('SetModelRoute', kind, model); }
export function getWorkspace() { return invoke<string>('GetWorkspace'); }
export function setWorkspace(dir: string) { return invoke<void>('SetWorkspace', dir); }

export interface ToolFamilyInfo {
  id: string;
  tools: string[];
  default_enabled: boolean;
  enabled?: boolean;
}
export function listToolFamilies() { return invoke<ToolFamilyInfo[]>('ListToolFamilies'); }

export function getExtensionConfig(name: string) { return invoke<Record<string, unknown>>('GetExtensionConfig', name); }
export function setExtensionConfig(name: string, cfg: Record<string, unknown>) { return invoke<void>('SetExtensionConfig', name, cfg); }

// ── MCP Server Management ───────────────────────────────────────────────

export interface MCPServerInfo {
  name: string;
  transport: string;
  command?: string;
  args?: string[];
  url?: string;
  enabled: boolean;
  connected: boolean;
  toolCount: number;
}
export interface MCPServerStatus {
  name: string;
  transport: string;
  enabled: boolean;
  connected: boolean;
  tools?: { name: string; description: string; parameters: unknown }[];
}
export function listMCPServers() { return invoke<MCPServerInfo[]>('ListMCPServers'); }
export function installMCPServer(name: string, transport: string, command: string, url: string, args: string[]) {
  return invoke<void>('InstallMCPServer', name, transport, command, url, args);
}
export function uninstallMCPServer(name: string) { return invoke<void>('UninstallMCPServer', name); }
export function connectMCPServer(name: string) { return invoke<void>('ConnectMCPServer', name); }
export function disconnectMCPServer(name: string) { return invoke<void>('DisconnectMCPServer', name); }
export function getMCPServerStatus(name: string) { return invoke<MCPServerStatus>('GetMCPServerStatus', name); }
export function callMCPTool(serverName: string, toolName: string, args: Record<string, unknown>) {
  return invoke<{ output: string; isError: boolean }>('CallMCPTool', serverName, toolName, args);
}

// ── Channels ──────────────────────────────────────────────────────────

export interface ChannelInfo {
  name: string;
  enabled: boolean;
  type: string;
}
export function listChannels() { return invoke<{ ok: boolean; channels: ChannelInfo[]; available: string[] }>('ListChannels'); }
export function enableChannel(name: string) { return invoke<{ ok: boolean; error?: string }>('EnableChannel', name); }
export function disableChannel(name: string) { return invoke<{ ok: boolean; error?: string }>('DisableChannel', name); }

// ── Webhook Tunnels ───────────────────────────────────────────────────

export interface TunnelInfo {
  id: string;
  tunnel_uuid: string;
  target: string;
  target_id: string;
  description: string;
  enabled: boolean;
  created_at: string;
}
export function listTunnels() { return invoke<{ ok: boolean; tunnels: TunnelInfo[] }>('ListTunnels'); }
export function createTunnel(target: string, targetId: string, description: string) {
  return invoke<{ ok: boolean; tunnel: TunnelInfo }>('CreateTunnel', target, targetId, description);
}
export function getTunnel(uuid: string) { return invoke<{ ok: boolean; tunnel: TunnelInfo }>('GetTunnel', uuid); }
export function updateTunnel(uuid: string, enabled?: boolean, description?: string) {
  return invoke<{ ok: boolean }>('UpdateTunnel', uuid, enabled, description);
}
export function deleteTunnel(uuid: string) { return invoke<{ ok: boolean }>('DeleteTunnel', uuid); }
export function getTunnelBandwidth(uuid: string) { return invoke<{ ok: boolean; bandwidth: Record<string, number> }>('GetTunnelBandwidth', uuid); }
export function listTunnelActivity(limit?: number) { return invoke<{ ok: boolean; activities: unknown[] }>('ListTunnelActivity', limit || 50); }
export function clearTunnelActivity() { return invoke<{ ok: boolean }>('ClearTunnelActivity'); }

// ── System ────────────────────────────────────────────────────────────

export interface SystemInfo {
  ok: boolean;
  workspace: string;
  model: string;
  tier: string;
  go_version: string;
}
export function getSystemInfo() { return invoke<SystemInfo>('SystemInfo'); }

export interface ModelInfo {
  ok: boolean;
  models: string[];
  default_model: string;
  provider_count: number;
}
export function listModels() { return invoke<ModelInfo>('ListModels').catch(() => ({ default_model: '', provider_count: 0 }) as ModelInfo); }

export interface InferenceDiagnostics {
  ok: boolean;
  provider_ready: boolean;
  default_model: string;
  max_output_tokens: number;
  temperature: number;
  provider_name?: string;
  context_guard_ok?: boolean;
}
export function getInferenceDiagnostics() { return invoke<InferenceDiagnostics>('InferenceDiagnostics'); }
export function getInferenceStatus() { return invoke<Record<string, unknown>>('InferenceStatus'); }
export function getProviderModels() { return invoke<Record<string, unknown>>('ProviderModels'); }

// ── Agent Registry (extended) ──────────────────────────────────────────

export interface AgentDetail {
  id: string;
  name: string;
  description: string;
  system_prompt: string;
  tier: string;
  hidden?: boolean;
  enabled?: boolean;
  tool_allowlist?: string[];
  tool_denylist?: string[];
}
export function getAgentDetail(id: string) { return invoke<{ ok: boolean; agent: AgentDetail }>('GetAgentDetail'); }
export function setAgentEnabled(id: string, enabled: boolean) { return invoke<{ ok: boolean }>('SetAgentEnabled', id, enabled); }
export function getAgentTools(id: string) { return invoke<{ ok: boolean; tools: string[] }>('GetAgentTools', id); }
export function deleteAgent(id: string) { return invoke<{ ok: boolean }>('DeleteAgent', id); }

// ── Extensions ───────────────────────────────────────────────

export interface ExtensionInfo {
  name: string;
  version: string;
  category: string;
  description: string;
  installPath: string;
  enabled: boolean;
  loaded: boolean;
  health: string;
  author: string;
  homepage: string;
}

export function listExtensions() { return invoke<ExtensionInfo[]>('ListExtensions'); }
export function installExtension(packagePath: string) { return invoke<void>('InstallExtension', packagePath); }
export function uninstallExtension(category: string, name: string) { return invoke<void>('UninstallExtension', category, name); }

export function listNamespaces() { return invoke<string[]>('ListNamespaces'); }
export function clearNamespace(ns: string) { return invoke<void>('ClearNamespace', ns); }

// ── Capability Registry ─────────────────────────────────────────────────

export interface CapabilitySet {
  id: string;
  name: string;
  kind: string;
  description?: string;
  tools: ToolDescriptor[];
  agents: AgentDescriptor[];
  health: string;
  enabled: boolean;
  connected_at?: string;
  tool_count: number;
  agent_count: number;
  config?: unknown;
}

export interface ToolDescriptor {
  name: string;
  description: string;
  permission: string;
  has_side_effects: boolean;
  is_concurrency_safe: boolean;
  input_schema: unknown;
}

export interface AgentDescriptor {
  id: string;
  name: string;
  description: string;
  tier: string;
  tool_allowlist?: string[];
  tool_denylist?: string[];
  max_iterations: number;
  hidden: boolean;
  model?: string;
  temperature?: number;
  timeout_secs?: number;
  sandbox_mode?: string;
  background?: boolean;
}

export function listCapabilitySets() { return invoke<CapabilitySet[]>('ListSets'); }
export function getCapabilitySet(id: string) { return invoke<CapabilitySet>('GetSet', id); }
export function listAllTools() { return invoke<ToolDescriptor[]>('ListAllTools'); }
export function getToolSchema(name: string) { return invoke<unknown>('GetToolSchema', name); }
export function listAllAgents() { return invoke<AgentDescriptor[]>('ListAllAgents'); }
export function addMCPServer(name: string, transport: string, command: string, url: string, args: string[]) {
  return invoke<void>('AddMCPServer', name, transport, command, url, args);
}
export function removeMCPServer(name: string) { return invoke<void>('RemoveMCPServer', name); }
export function connectCapMCPServer(name: string) { return invoke<void>('ConnectMCPServer', name); }
export function disconnectCapMCPServer(name: string) { return invoke<void>('DisconnectMCPServer', name); }

export function setRetrievalProfile(profile: string) { return invoke<void>('SetRetrievalProfile', profile); }

export function installLegacyExtension(sourcePath: string) { return invoke<void>('InstallLegacyExtension', sourcePath); }
export function installSkill(url: string) { return invoke<void>('InstallSkill', url); }

export interface SkillCatalogEntry {
  id: string;
  name: string;
  description: string;
  version?: string;
  author?: string;
  source: string;
  download_url: string;
  tags?: string[];
  license?: string;
}

export function listSkillCatalog() { return invoke<SkillCatalogEntry[]>('ListSkillCatalog'); }
export function refreshSkillCatalog() { return invoke<SkillCatalogEntry[]>('RefreshSkillCatalog'); }
export function installSkillFromCatalog(entryId: string) { return invoke<void>('InstallSkillFromCatalog', entryId); }
export function getSOUL() { return invoke<string>('GetSOUL'); }
export function setSOUL(content: string) { return invoke<void>('SetSOUL', content); }
export function getIdentity() { return invoke<string>('GetIdentity'); }
export function setIdentity(content: string) { return invoke<void>('SetIdentity', content); }

// ── Workflow ──────────────────────────────────────────────────────────
export function installWorkflowFromURL(url: string) { return invoke<string>('InstallWorkflowFromURL', url); }
export function uninstallWorkflow(name: string) { return invoke<void>('UninstallWorkflow', name); }
