export namespace about {
	
	export class RPC {
	
	
	    static createFrom(source: any = {}) {
	        return new RPC(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace agent {
	
	export class AgentRPC {
	
	
	    static createFrom(source: any = {}) {
	        return new AgentRPC(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class ChatService {
	
	
	    static createFrom(source: any = {}) {
	        return new ChatService(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class SessionDB {
	
	
	    static createFrom(source: any = {}) {
	        return new SessionDB(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace app_state {
	
	export class AppStateRPC {
	
	
	    static createFrom(source: any = {}) {
	        return new AppStateRPC(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace approval {
	
	export class ApprovalRPC {
	
	
	    static createFrom(source: any = {}) {
	        return new ApprovalRPC(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class Gate {
	
	
	    static createFrom(source: any = {}) {
	        return new Gate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace capability {
	
	export class AgentDescriptor {
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
	
	    static createFrom(source: any = {}) {
	        return new AgentDescriptor(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.tier = source["tier"];
	        this.tool_allowlist = source["tool_allowlist"];
	        this.tool_denylist = source["tool_denylist"];
	        this.max_iterations = source["max_iterations"];
	        this.hidden = source["hidden"];
	        this.model = source["model"];
	        this.temperature = source["temperature"];
	        this.timeout_secs = source["timeout_secs"];
	        this.sandbox_mode = source["sandbox_mode"];
	        this.background = source["background"];
	    }
	}
	export class CapabilityRPC {
	
	
	    static createFrom(source: any = {}) {
	        return new CapabilityRPC(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class CapabilityRegistry {
	
	
	    static createFrom(source: any = {}) {
	        return new CapabilityRegistry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class ToolDescriptor {
	    name: string;
	    description: string;
	    permission: string;
	    has_side_effects: boolean;
	    is_concurrency_safe: boolean;
	    input_schema: number[];
	
	    static createFrom(source: any = {}) {
	        return new ToolDescriptor(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.permission = source["permission"];
	        this.has_side_effects = source["has_side_effects"];
	        this.is_concurrency_safe = source["is_concurrency_safe"];
	        this.input_schema = source["input_schema"];
	    }
	}
	export class CapabilitySet {
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
	    channel_count?: number;
	    config?: number[];
	
	    static createFrom(source: any = {}) {
	        return new CapabilitySet(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.description = source["description"];
	        this.tools = this.convertValues(source["tools"], ToolDescriptor);
	        this.agents = this.convertValues(source["agents"], AgentDescriptor);
	        this.health = source["health"];
	        this.enabled = source["enabled"];
	        this.connected_at = source["connected_at"];
	        this.tool_count = source["tool_count"];
	        this.agent_count = source["agent_count"];
	        this.channel_count = source["channel_count"];
	        this.config = source["config"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExtensionInfo {
	    name: string;
	    version: string;
	    category: string;
	    description?: string;
	    installPath: string;
	    enabled: boolean;
	    loaded: boolean;
	    health: string;
	    author?: string;
	    homepage?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExtensionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.version = source["version"];
	        this.category = source["category"];
	        this.description = source["description"];
	        this.installPath = source["installPath"];
	        this.enabled = source["enabled"];
	        this.loaded = source["loaded"];
	        this.health = source["health"];
	        this.author = source["author"];
	        this.homepage = source["homepage"];
	    }
	}

}

export namespace channels {
	
	export class ChannelRPC {
	
	
	    static createFrom(source: any = {}) {
	        return new ChannelRPC(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace config {
	
	export class ActionDirConfig {
	    override?: string;
	
	    static createFrom(source: any = {}) {
	        return new ActionDirConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.override = source["override"];
	    }
	}
	export class AgentLimits {
	    max_tool_rounds: number;
	    default_tool_timeout: number;
	
	    static createFrom(source: any = {}) {
	        return new AgentLimits(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.max_tool_rounds = source["max_tool_rounds"];
	        this.default_tool_timeout = source["default_tool_timeout"];
	    }
	}
	export class AgentConfig {
	    DefaultModel: string;
	    MaxOutputTokens: number;
	    Temperature: number;
	    ModelRoutes: Record<string, string>;
	    Limits: AgentLimits;
	
	    static createFrom(source: any = {}) {
	        return new AgentConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.DefaultModel = source["DefaultModel"];
	        this.MaxOutputTokens = source["MaxOutputTokens"];
	        this.Temperature = source["Temperature"];
	        this.ModelRoutes = source["ModelRoutes"];
	        this.Limits = this.convertValues(source["Limits"], AgentLimits);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class TrustedRootEntry {
	    Path: string;
	    Access: string;
	
	    static createFrom(source: any = {}) {
	        return new TrustedRootEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Path = source["Path"];
	        this.Access = source["Access"];
	    }
	}
	export class AutonomyConfig {
	    level: string;
	    workspace_only: boolean;
	    allowed_commands: string[];
	    forbidden_paths: string[];
	    max_actions_per_hour: number;
	    max_cost_per_day_cents: number;
	    require_approval_for_medium_risk: boolean;
	    block_high_risk_commands: boolean;
	    auto_approve: string[];
	    trusted_roots: TrustedRootEntry[];
	    allow_tool_install: boolean;
	    require_task_plan_approval: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AutonomyConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.level = source["level"];
	        this.workspace_only = source["workspace_only"];
	        this.allowed_commands = source["allowed_commands"];
	        this.forbidden_paths = source["forbidden_paths"];
	        this.max_actions_per_hour = source["max_actions_per_hour"];
	        this.max_cost_per_day_cents = source["max_cost_per_day_cents"];
	        this.require_approval_for_medium_risk = source["require_approval_for_medium_risk"];
	        this.block_high_risk_commands = source["block_high_risk_commands"];
	        this.auto_approve = source["auto_approve"];
	        this.trusted_roots = this.convertValues(source["trusted_roots"], TrustedRootEntry);
	        this.allow_tool_install = source["allow_tool_install"];
	        this.require_task_plan_approval = source["require_task_plan_approval"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class BundleConfig {
	    disabled: string[];
	
	    static createFrom(source: any = {}) {
	        return new BundleConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.disabled = source["disabled"];
	    }
	}
	export class ChannelConfig {
	    Disabled: boolean;
	    Token: string;
	    WebhookURL: string;
	    Extra: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new ChannelConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Disabled = source["Disabled"];
	        this.Token = source["Token"];
	        this.WebhookURL = source["WebhookURL"];
	        this.Extra = source["Extra"];
	    }
	}
	export class CircuitBreakerConfig {
	    max_repeat_failures: number;
	    max_no_progress_fails: number;
	
	    static createFrom(source: any = {}) {
	        return new CircuitBreakerConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.max_repeat_failures = source["max_repeat_failures"];
	        this.max_no_progress_fails = source["max_no_progress_fails"];
	    }
	}
	export class InferenceHTTPConfig {
	    enabled: boolean;
	    port: number;
	    bind: string;
	
	    static createFrom(source: any = {}) {
	        return new InferenceHTTPConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.port = source["port"];
	        this.bind = source["bind"];
	    }
	}
	export class ScreenIntelligenceConfig {
	    enabled: boolean;
	    capture_interval_secs: number;
	
	    static createFrom(source: any = {}) {
	        return new ScreenIntelligenceConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.capture_interval_secs = source["capture_interval_secs"];
	    }
	}
	export class ProxyConfig {
	    http_proxy: string;
	    https_proxy: string;
	    no_proxy: string;
	
	    static createFrom(source: any = {}) {
	        return new ProxyConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.http_proxy = source["http_proxy"];
	        this.https_proxy = source["https_proxy"];
	        this.no_proxy = source["no_proxy"];
	    }
	}
	export class ObservabilityConfig {
	    log_level: string;
	
	    static createFrom(source: any = {}) {
	        return new ObservabilityConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.log_level = source["log_level"];
	    }
	}
	export class SandboxConfig {
	    mode: string;
	    backend_override?: string;
	
	    static createFrom(source: any = {}) {
	        return new SandboxConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.backend_override = source["backend_override"];
	    }
	}
	export class CostConfig {
	    budget_cents: number;
	
	    static createFrom(source: any = {}) {
	        return new CostConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.budget_cents = source["budget_cents"];
	    }
	}
	export class ToolsShellConfig {
	    max_output_bytes: number;
	    safe_env_vars: string[];
	
	    static createFrom(source: any = {}) {
	        return new ToolsShellConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.max_output_bytes = source["max_output_bytes"];
	        this.safe_env_vars = source["safe_env_vars"];
	    }
	}
	export class ToolsConfig {
	    Shell: ToolsShellConfig;
	    OptionalTools: string[];
	
	    static createFrom(source: any = {}) {
	        return new ToolsConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Shell = this.convertValues(source["Shell"], ToolsShellConfig);
	        this.OptionalTools = source["OptionalTools"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CronConfig {
	    HeartbeatIntervalSecs: number;
	
	    static createFrom(source: any = {}) {
	        return new CronConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.HeartbeatIntervalSecs = source["HeartbeatIntervalSecs"];
	    }
	}
	export class LearningConfig {
	    Enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LearningConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Enabled = source["Enabled"];
	    }
	}
	export class WebhookConfig {
	    Enabled: boolean;
	    Port: number;
	    Secret: string;
	
	    static createFrom(source: any = {}) {
	        return new WebhookConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Enabled = source["Enabled"];
	        this.Port = source["Port"];
	        this.Secret = source["Secret"];
	    }
	}
	export class VoiceConfig {
	    STTProvider: string;
	    STTModel: string;
	    STTEndpoint: string;
	    STTAPIKey: string;
	    TTSProvider: string;
	    TTSModel: string;
	    TTSEndpoint: string;
	    TTSAPIKey: string;
	
	    static createFrom(source: any = {}) {
	        return new VoiceConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.STTProvider = source["STTProvider"];
	        this.STTModel = source["STTModel"];
	        this.STTEndpoint = source["STTEndpoint"];
	        this.STTAPIKey = source["STTAPIKey"];
	        this.TTSProvider = source["TTSProvider"];
	        this.TTSModel = source["TTSModel"];
	        this.TTSEndpoint = source["TTSEndpoint"];
	        this.TTSAPIKey = source["TTSAPIKey"];
	    }
	}
	export class EmbeddingConfig {
	    Provider: string;
	    Model: string;
	    BaseURL: string;
	    APIKey: string;
	
	    static createFrom(source: any = {}) {
	        return new EmbeddingConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Provider = source["Provider"];
	        this.Model = source["Model"];
	        this.BaseURL = source["BaseURL"];
	        this.APIKey = source["APIKey"];
	    }
	}
	export class RetrievalWeightsConfig {
	    profile: string;
	    fts5: number;
	    vector: number;
	    keyword: number;
	    tree: number;
	    graph: number;
	    episodic: number;
	
	    static createFrom(source: any = {}) {
	        return new RetrievalWeightsConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.profile = source["profile"];
	        this.fts5 = source["fts5"];
	        this.vector = source["vector"];
	        this.keyword = source["keyword"];
	        this.tree = source["tree"];
	        this.graph = source["graph"];
	        this.episodic = source["episodic"];
	    }
	}
	export class MemoryPipelineConfig {
	    worker_count: number;
	    tree_bucket_size: number;
	    archive_msg_limit: number;
	    freshness_half_life: number;
	
	    static createFrom(source: any = {}) {
	        return new MemoryPipelineConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.worker_count = source["worker_count"];
	        this.tree_bucket_size = source["tree_bucket_size"];
	        this.archive_msg_limit = source["archive_msg_limit"];
	        this.freshness_half_life = source["freshness_half_life"];
	    }
	}
	export class MemoryConfig {
	    MaxChunkSize: number;
	    RetentionDays: number;
	    MaxSearchResults: number;
	    Pipeline: MemoryPipelineConfig;
	    RetrievalWeights: RetrievalWeightsConfig;
	
	    static createFrom(source: any = {}) {
	        return new MemoryConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.MaxChunkSize = source["MaxChunkSize"];
	        this.RetentionDays = source["RetentionDays"];
	        this.MaxSearchResults = source["MaxSearchResults"];
	        this.Pipeline = this.convertValues(source["Pipeline"], MemoryPipelineConfig);
	        this.RetrievalWeights = this.convertValues(source["RetrievalWeights"], RetrievalWeightsConfig);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SearchConfig {
	    BraveAPIKey: string;
	    TavilyAPIKey: string;
	    SearxNGURL: string;
	
	    static createFrom(source: any = {}) {
	        return new SearchConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.BraveAPIKey = source["BraveAPIKey"];
	        this.TavilyAPIKey = source["TavilyAPIKey"];
	        this.SearxNGURL = source["SearxNGURL"];
	    }
	}
	export class MCPServerEntry {
	    Name: string;
	    Transport: string;
	    Command: string;
	    Args: string[];
	    URL: string;
	    Enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MCPServerEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.Transport = source["Transport"];
	        this.Command = source["Command"];
	        this.Args = source["Args"];
	        this.URL = source["URL"];
	        this.Enabled = source["Enabled"];
	    }
	}
	export class ProviderConfig {
	    name: string;
	    type: string;
	    api_key: string;
	    base_url: string;
	    models: string[];
	
	    static createFrom(source: any = {}) {
	        return new ProviderConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.api_key = source["api_key"];
	        this.base_url = source["base_url"];
	        this.models = source["models"];
	    }
	}
	export class SecurityCommands {
	    block_high_risk: boolean;
	    require_medium_approval: boolean;
	    extra_read_only: string[];
	    extra_network: string[];
	    extra_destructive: string[];
	    extra_install: string[];
	    extra_executors: string[];
	    extra_dangerous_env: string[];
	    extra_allowed_commands: string[];
	
	    static createFrom(source: any = {}) {
	        return new SecurityCommands(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.block_high_risk = source["block_high_risk"];
	        this.require_medium_approval = source["require_medium_approval"];
	        this.extra_read_only = source["extra_read_only"];
	        this.extra_network = source["extra_network"];
	        this.extra_destructive = source["extra_destructive"];
	        this.extra_install = source["extra_install"];
	        this.extra_executors = source["extra_executors"];
	        this.extra_dangerous_env = source["extra_dangerous_env"];
	        this.extra_allowed_commands = source["extra_allowed_commands"];
	    }
	}
	export class SecurityConfig {
	    Tier: string;
	    WorkspaceOnly: boolean;
	    TrustedRoots: TrustedRootEntry[];
	    ForbiddenPaths: string[];
	    Commands: SecurityCommands;
	
	    static createFrom(source: any = {}) {
	        return new SecurityConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Tier = source["Tier"];
	        this.WorkspaceOnly = source["WorkspaceOnly"];
	        this.TrustedRoots = this.convertValues(source["TrustedRoots"], TrustedRootEntry);
	        this.ForbiddenPaths = source["ForbiddenPaths"];
	        this.Commands = this.convertValues(source["Commands"], SecurityCommands);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Config {
	    Workspace: string;
	    SchemaVersion: number;
	    Agent: AgentConfig;
	    Security: SecurityConfig;
	    Providers: ProviderConfig[];
	    Channels: Record<string, ChannelConfig>;
	    MCPServers: MCPServerEntry[];
	    Search: SearchConfig;
	    Memory: MemoryConfig;
	    Embedding: EmbeddingConfig;
	    Voice: VoiceConfig;
	    Webhook: WebhookConfig;
	    Learning: LearningConfig;
	    Cron: CronConfig;
	    Tools: ToolsConfig;
	    CircuitBreaker: CircuitBreakerConfig;
	    Cost: CostConfig;
	    Autonomy: AutonomyConfig;
	    Sandbox: SandboxConfig;
	    ActionDir: ActionDirConfig;
	    Observability: ObservabilityConfig;
	    Proxy: ProxyConfig;
	    ScreenIntelligence: ScreenIntelligenceConfig;
	    InferenceHTTP: InferenceHTTPConfig;
	    Bundles: BundleConfig;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Workspace = source["Workspace"];
	        this.SchemaVersion = source["SchemaVersion"];
	        this.Agent = this.convertValues(source["Agent"], AgentConfig);
	        this.Security = this.convertValues(source["Security"], SecurityConfig);
	        this.Providers = this.convertValues(source["Providers"], ProviderConfig);
	        this.Channels = this.convertValues(source["Channels"], ChannelConfig, true);
	        this.MCPServers = this.convertValues(source["MCPServers"], MCPServerEntry);
	        this.Search = this.convertValues(source["Search"], SearchConfig);
	        this.Memory = this.convertValues(source["Memory"], MemoryConfig);
	        this.Embedding = this.convertValues(source["Embedding"], EmbeddingConfig);
	        this.Voice = this.convertValues(source["Voice"], VoiceConfig);
	        this.Webhook = this.convertValues(source["Webhook"], WebhookConfig);
	        this.Learning = this.convertValues(source["Learning"], LearningConfig);
	        this.Cron = this.convertValues(source["Cron"], CronConfig);
	        this.Tools = this.convertValues(source["Tools"], ToolsConfig);
	        this.CircuitBreaker = this.convertValues(source["CircuitBreaker"], CircuitBreakerConfig);
	        this.Cost = this.convertValues(source["Cost"], CostConfig);
	        this.Autonomy = this.convertValues(source["Autonomy"], AutonomyConfig);
	        this.Sandbox = this.convertValues(source["Sandbox"], SandboxConfig);
	        this.ActionDir = this.convertValues(source["ActionDir"], ActionDirConfig);
	        this.Observability = this.convertValues(source["Observability"], ObservabilityConfig);
	        this.Proxy = this.convertValues(source["Proxy"], ProxyConfig);
	        this.ScreenIntelligence = this.convertValues(source["ScreenIntelligence"], ScreenIntelligenceConfig);
	        this.InferenceHTTP = this.convertValues(source["InferenceHTTP"], InferenceHTTPConfig);
	        this.Bundles = this.convertValues(source["Bundles"], BundleConfig);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ConfigRPC {
	
	
	    static createFrom(source: any = {}) {
	        return new ConfigRPC(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	

}

export namespace conversations {
	
	export class Store {
	
	
	    static createFrom(source: any = {}) {
	        return new Store(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace cost {
	
	export class Tracker {
	
	
	    static createFrom(source: any = {}) {
	        return new Tracker(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace cron {
	
	export class CronRPC {
	
	
	    static createFrom(source: any = {}) {
	        return new CronRPC(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class Scheduler {
	
	
	    static createFrom(source: any = {}) {
	        return new Scheduler(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace desktop {
	
	export class CompanionLoop {
	
	
	    static createFrom(source: any = {}) {
	        return new CompanionLoop(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class ScreenIntelLoop {
	
	
	    static createFrom(source: any = {}) {
	        return new ScreenIntelLoop(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace health {
	
	export class Registry {
	
	
	    static createFrom(source: any = {}) {
	        return new Registry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace learning {
	
	export class Engine {
	
	
	    static createFrom(source: any = {}) {
	        return new Engine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace main {
	
	export class ChatRequest {
	    threadId: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new ChatRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.threadId = source["threadId"];
	        this.message = source["message"];
	    }
	}
	export class ChatResponse {
	    threadId: string;
	    content: string;
	    done: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ChatResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.threadId = source["threadId"];
	        this.content = source["content"];
	        this.done = source["done"];
	    }
	}
	export class todoCard {
	    id: string;
	    title: string;
	    status: string;
	    order: number;
	    notes?: string;
	    assignedTo?: string;
	    createdAt: string;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new todoCard(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.status = source["status"];
	        this.order = source["order"];
	        this.notes = source["notes"];
	        this.assignedTo = source["assignedTo"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class todoSnapshot {
	    threadId: string;
	    cards: todoCard[];
	    markdown: string;
	
	    static createFrom(source: any = {}) {
	        return new todoSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.threadId = source["threadId"];
	        this.cards = this.convertValues(source["cards"], todoCard);
	        this.markdown = source["markdown"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace memory {
	
	export class Pipeline {
	
	
	    static createFrom(source: any = {}) {
	        return new Pipeline(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace monitor {
	
	export class RunSummary {
	    id: string;
	    command: string;
	    status: string;
	    exit_code: number;
	    error?: string;
	    started_at: number;
	    ended_at?: number;
	
	    static createFrom(source: any = {}) {
	        return new RunSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.command = source["command"];
	        this.status = source["status"];
	        this.exit_code = source["exit_code"];
	        this.error = source["error"];
	        this.started_at = source["started_at"];
	        this.ended_at = source["ended_at"];
	    }
	}
	export class ListRunsResult {
	    runs: RunSummary[];
	    total: number;
	
	    static createFrom(source: any = {}) {
	        return new ListRunsResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.runs = this.convertValues(source["runs"], RunSummary);
	        this.total = source["total"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Manager {
	
	
	    static createFrom(source: any = {}) {
	        return new Manager(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	
	export class StartRunRequest {
	    command: string;
	    timeout_secs: number;
	    use_pty: boolean;
	
	    static createFrom(source: any = {}) {
	        return new StartRunRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.command = source["command"];
	        this.timeout_secs = source["timeout_secs"];
	        this.use_pty = source["use_pty"];
	    }
	}
	export class StartRunResult {
	    run_id: string;
	
	    static createFrom(source: any = {}) {
	        return new StartRunResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.run_id = source["run_id"];
	    }
	}

}

export namespace notifications {
	
	export class Bus {
	
	
	    static createFrom(source: any = {}) {
	        return new Bus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class Notification {
	    id: string;
	    kind: string;
	    title: string;
	    body: string;
	    thread_id?: string;
	    channel?: string;
	    read: boolean;
	    // Go type: time
	    created_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Notification(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.body = source["body"];
	        this.thread_id = source["thread_id"];
	        this.channel = source["channel"];
	        this.read = source["read"];
	        this.created_at = this.convertValues(source["created_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace prompts {
	
	export class PromptMeta {
	    name: string;
	    description: string;
	    length: number;
	    overridden: boolean;
	    default_length: number;
	    builtin: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PromptMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.length = source["length"];
	        this.overridden = source["overridden"];
	        this.default_length = source["default_length"];
	        this.builtin = source["builtin"];
	    }
	}

}

export namespace security {
	
	export class AuditEvent {
	    id: string;
	    kind: string;
	    tool_name?: string;
	    command?: string;
	    args?: string;
	    decision?: string;
	    reason?: string;
	    // Go type: time
	    created_at: any;
	
	    static createFrom(source: any = {}) {
	        return new AuditEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.tool_name = source["tool_name"];
	        this.command = source["command"];
	        this.args = source["args"];
	        this.decision = source["decision"];
	        this.reason = source["reason"];
	        this.created_at = this.convertValues(source["created_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AuditLogger {
	
	
	    static createFrom(source: any = {}) {
	        return new AuditLogger(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace slog {
	
	export class Logger {
	
	
	    static createFrom(source: any = {}) {
	        return new Logger(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace sql {
	
	export class DB {
	
	
	    static createFrom(source: any = {}) {
	        return new DB(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace webhooks {
	
	export class TunnelManager {
	
	
	    static createFrom(source: any = {}) {
	        return new TunnelManager(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

