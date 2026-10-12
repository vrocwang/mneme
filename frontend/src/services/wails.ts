// Typed wrappers for Go backend Wails bindings.
// Methods are exposed under window['go']['<package>']['<Struct>'] by the Wails
// runtime; rpcRoutes.generated.ts maps each method name to its owner.

import { rpcRoutes } from './rpcRoutes.generated';
import { RPC_TIMEOUT_MS, STREAM_TIMEOUT_MS } from '../lib/timing';
import type { main } from '../../wailsjs/go/models';

export class RpcError extends Error {
  constructor(message: string, public code?: string, public status?: number) {
    super(message);
    this.name = 'RpcError';
  }
}

function invoke<T>(method: string, ...args: unknown[]): Promise<T> {
  const w = window as any;
  // Routing comes from the generated table (scripts/gen-rpc-routes.mjs), which is
  // derived from the Wails bindings and checked in CI against every call site in
  // src/. A miss means the Go method is not bound (or is wiring/lifecycle rather
  // than an RPC) — there is no fallback, so a typo cannot silently reach an
  // unrelated struct. Browser mode (no Wails runtime) rejects the same way.
  const route = rpcRoutes[method];
  const fn = route ? w['go']?.[route.pkg]?.[route.struct]?.[method] : undefined;
  if (!fn) {
    console.warn(`Wails method ${method} not available${route ? ` at go.${route.pkg}.${route.struct}` : ' (not in the RPC route table)'} — running in browser mode?`);
    return Promise.reject(new RpcError(`Wails method ${method} not available`, 'NOT_AVAILABLE'));
  }
  return fn(...args) as Promise<T>;
}

// invokeWithTimeout wraps invoke with a configurable timeout.
async function invokeWithTimeout<T>(
  method: string,
  args: unknown[],
  timeoutMs: number = RPC_TIMEOUT_MS,
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

// ── Chat ──────────────────────────────────────────────────────────────

export interface ChatRequest {
  threadId: string;
  message: string;
}
// streamChatMessage initiates a streaming chat turn. Tokens arrive via Wails
// events: "chat:token", "chat:tool_call", "chat:tool_result", "chat:done", "chat:error".
// Uses a 120s timeout for long-running agent turns.
export function streamChatMessage(req: ChatRequest) {
  return invokeWithTimeout<void>('StreamChatMessage', [req], STREAM_TIMEOUT_MS);
}

// Cancel the in-flight turn for a thread. It routes through the generated table
// like every other method (rpcRoutes.generated.ts).
export function cancelMessage(threadId: string) {
  return invoke<void>('CancelMessage', threadId);
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
  // Set on the locally committed assistant message of a turn the user stopped.
  stopped?: boolean;
}
export function listThreads() { return invoke<ThreadSummary[]>('ListThreads'); }
export function createThread(title: string) { return invoke<ThreadSummary>('CreateThread', title); }
export function deleteThread(threadId: string) { return invoke<void>('DeleteThread', threadId); }
export function getThreadMessages(threadId: string, limit?: number, afterId?: number) {
  return invoke<MessageRecord[]>('GetThreadMessages', threadId, limit ?? 200, afterId ?? 0);
 }

// ── Todo board ────────────────────────────────────────────────────────

// The wire types are the generated Wails bindings (main.todoSnapshot /
// main.todoCard), which mirror the Go JSON tags (threadId/cards/markdown,
// createdAt/updatedAt). Hand-declaring them here had already drifted
// (thread_id/created_at), so the UI could read a field the backend never sends.
export type TodoSnapshot = main.todoSnapshot;
export type TodoCard = main.todoCard;
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
  maxIterations?: number;
  hidden?: boolean;
  forkMode?: boolean;
  timeoutSecs?: number;
  sandboxMode?: string;
}
export function upsertAgent(
  id: string, name: string, description: string, systemPrompt: string,
  tier: string, model: string, maxIterations: number, toolAllowlist: string[],
) {
  return invoke<void>('UpsertAgent', id, name, description, systemPrompt, tier, model, maxIterations, toolAllowlist);
}
export function removeAgent(id: string) { return invoke<void>('RemoveAgent', id); }

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
  mcpHTTPTools: number;
  jsonRpcTools: number;
  inProcessTools: number;
  writeSurfaces: number;
  bySource: Record<string, number>;
  // Tool denials are not reported here; read them from the audit trail with
  // listAuditEventsByKind('tool_blocked', limit).
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
export function getCostDashboard() { return invoke<CostDashboard>('GetCostDashboard'); }

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
export function getCronJobs() { return invoke<unknown[]>('GetCronJobs'); }
export function toggleCronJob(id: string, enabled: boolean) { return invoke<{ ok: boolean; error?: string }>('ToggleCronJob', id, enabled); }
export function triggerCronJob(id: string) { return invoke<{ ok: boolean; error?: string }>('TriggerCronJob', id); }
export function addCronJob(name: string, schedule: string, prompt: string) { return invoke<{ ok: boolean; id?: string; error?: string }>('AddCronJob', name, schedule, prompt); }
export function removeCronJob(id: string) { return invoke<{ ok: boolean; error?: string }>('RemoveCronJob', id); }
export function getPreferences() { return invoke<unknown[]>('GetPreferences'); }
export function getReflections(limit: number) { return invoke<unknown[]>('GetReflections', limit); }

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

// ── Updates ───────────────────────────────────────────────────────────

export function checkForUpdate() { return invoke<unknown>('CheckForUpdate'); }
export function getCurrentVersion() { return invoke<string>('GetCurrentVersion'); }

// ── Config ──────────────────────────────────────────────────────────────

export interface AgentLimitsConfig {
  max_tool_rounds: number;
  default_tool_timeout: number;
}
export interface SecurityCommandsConfig {
  block_high_risk: boolean;
  require_medium_approval: boolean;
  extra_read_only: string[];
  extra_network: string[];
  extra_destructive: string[];
  extra_install: string[];
  extra_executors: string[];
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
  // Named preset. When non-empty the backend applies the preset and ignores the
  // numeric weights, so any save path must carry it through unchanged.
  profile?: string;
}
export interface CostConfig { budget_cents: number; }

export function getAgentLimits() { return invoke<AgentLimitsConfig>('GetAgentLimits'); }
export function setAgentLimits(l: AgentLimitsConfig) { return invoke<void>('SetAgentLimits', l); }
export function setSecurityTier(tier: string) { return invoke<void>('SetSecurityTier', tier); }
export function getSecurityCommands() { return invoke<SecurityCommandsConfig>('GetSecurityCommands'); }
export function setSecurityCommands(c: SecurityCommandsConfig) { return invoke<void>('SetSecurityCommands', c); }
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

// ── Channels ──────────────────────────────────────────────────────────

export interface ChannelInfo {
  name: string;
  enabled: boolean;
  type: string;
  // Live state: whether the channel is actually running right now. An enabled
  // channel can still be stopped (startup failure, removed at runtime).
  running: boolean;
}
export function listChannels() { return invoke<{ ok: boolean; channels: ChannelInfo[]; available: string[] }>('ListChannels'); }
export function enableChannel(name: string) {
  return invoke<{ ok: boolean; name?: string; enabled?: boolean; error?: string }>('EnableChannel', name);
}
export function disableChannel(name: string) {
  return invoke<{ ok: boolean; name?: string; enabled?: boolean; error?: string }>('DisableChannel', name);
}

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
export function deleteTunnel(uuid: string) { return invoke<{ ok: boolean }>('DeleteTunnel', uuid); }
export function listTunnelActivity(limit?: number) { return invoke<{ ok: boolean; activities: unknown[] }>('ListTunnelActivity', limit || 50); }
export function clearTunnelActivity() { return invoke<{ ok: boolean }>('ClearTunnelActivity'); }

// ── System ────────────────────────────────────────────────────────────

export interface ModelInfo {
  ok: boolean;
  models: string[];
  default_model: string;
  provider_count: number;
}
export function listModels() { return invoke<ModelInfo>('ListModels').catch(() => ({ default_model: '', provider_count: 0 }) as ModelInfo); }

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
  /** Declared from a cached handshake: its process starts on first tool use. */
  lazy: boolean;
  /** False for a lazily declared extension that has not been called yet. */
  started: boolean;
}

export function listExtensions() { return invoke<ExtensionInfo[]>('ListExtensions'); }
export function installExtension(packagePath: string) { return invoke<void>('InstallExtension', packagePath); }
export function uninstallExtension(category: string, name: string) { return invoke<void>('UninstallExtension', category, name); }

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
  /** Declared from a cached handshake: its process starts on first tool use. */
  lazy?: boolean;
  /** False for a lazily declared extension that has not been called yet. */
  started?: boolean;
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
export function addMCPServer(name: string, transport: string, command: string, url: string, args: string[]) {
  return invoke<void>('AddMCPServer', name, transport, command, url, args);
}
export function removeMCPServer(name: string) { return invoke<void>('RemoveMCPServer', name); }
export function connectCapMCPServer(name: string) { return invoke<void>('ConnectMCPServer', name); }
export function disconnectCapMCPServer(name: string) { return invoke<void>('DisconnectMCPServer', name); }

export function setRetrievalProfile(profile: string) { return invoke<void>('SetRetrievalProfile', profile); }

export function installSkill(url: string) { return invoke<void>('InstallSkill', url); }

export function getSOUL() { return invoke<string>('GetSOUL'); }
export function setSOUL(content: string) { return invoke<void>('SetSOUL', content); }
export function getIdentity() { return invoke<string>('GetIdentity'); }
export function setIdentity(content: string) { return invoke<void>('SetIdentity', content); }

// ── Notifications (notifications.Bus) ──────────────────────────────────────
export interface NotificationRecord {
  id: string;
  kind: string;
  title: string;
  body: string;
  thread_id?: string;
  channel?: string;
  read: boolean;
  created_at: string;
}
export function listNotifications(limit = 50) { return invoke<NotificationRecord[]>('ListNotifications', limit); }
export function listUnreadNotifications(limit = 50) { return invoke<NotificationRecord[]>('ListUnreadNotifications', limit); }
export function markNotificationRead(id: string) { return invoke<void>('MarkNotificationRead', id); }
export function markAllNotificationsRead() { return invoke<void>('MarkAllNotificationsRead'); }
export function unreadNotificationCount() { return invoke<number>('UnreadNotificationCount'); }

// ── Security audit trail (security.AuditRPC) ───────────────────────────────
export interface AuditEventRecord {
  id: string;
  kind: string;
  tool_name?: string;
  command?: string;
  args?: string;
  decision?: string;
  reason?: string;
  created_at: string;
}
export function listAuditEvents(limit = 50) { return invoke<AuditEventRecord[]>('ListAuditEvents', limit); }
export function listAuditEventsByKind(kind: string, limit = 50) {
  return invoke<AuditEventRecord[]>('ListAuditEventsByKind', kind, limit);
}
