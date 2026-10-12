const en = {
  app: { title: 'Mneme' },

  common: {
    loading: 'Loading...',
    unavailable: 'unavailable',
    unknownError: 'Unknown error',
    apiKeyPlaceholder: 'sk-...',
    yes: 'Yes',
    no: 'No',
    internet: 'Internet',
    core: 'Core',
    api: 'API',
    themeTooltip: 'Theme: {current} (click for {next})'
  },

  nav: {
    home: 'Home',
    chat: 'Chat',
    memory: 'Memory',
    capabilities: 'Capabilities',
    dashboard: 'Dashboard',
    notifications: 'Notifications',
    monitor: 'Monitor',
    settings: 'Settings',
    tools: 'Tools'
  },

  sidebar: {
    threads: 'Threads',
    newThread: 'New thread',
    searchThreads: 'Search threads...',
    noThreads: 'No threads yet',
    noMatch: 'No matching threads',
    msgs: 'msgs',
    deleteThread: 'Delete thread',
    closeSidebar: 'Close sidebar',
    count: '{count} threads',
    openSidebar: 'Open sidebar'
  },

  chat: {
    newConversation: 'New conversation',
    selectThread: 'Select a thread or create a new one to start a conversation.',
    startConversation: 'Start a conversation',
    startHint: 'Send a message to begin',
    placeholder: 'Send a message... (Enter to send, Shift+Enter for new line)',
    
    
    
    you: 'You',
    assistant: 'Mneme',
    toolResult: 'Tool result',
    tool: 'Tool',
    copy: 'Copy',
    copied: 'Copied!',
    stop: 'Stop',
    runningTools: 'Running {count} tools...',
    runningTool: 'Running {count} tool...',
    stopping: 'Stopping...',
    stopped: 'Stopped',
    thinkingShow: 'Show thinking',
    thinkingHide: 'Hide thinking',
    delete: 'Delete',
    messages: '{count} messages',
    toolStatus: {
      pending: 'pending',
      running: 'running',
      done: 'done',
      error: 'error'
    }
  },

  approval: {
    required: 'Approval required',
    wantsToExecute: 'wants to execute',
    approveOnce: 'Approve once',
    alwaysAllow: 'Always allow',
    deny: 'Deny',
    decisionRecorded: 'Decision recorded',
    toolAllowed: 'Tool permanently allowed',
    approvedOnce: 'Approved once',
    denied: 'Denied',
    decideFailed: 'Failed to decide',
    
    
    
    
    pending: 'pending'
  },

  settings: {
    title: 'Settings',
    general: 'General',
    providers: 'Providers',
    
    approvals: 'Approvals',
    theme: 'Theme',
    config: 'Config',
    prompts: 'Prompts',
    about: 'About',
    systemHealth: 'System Health',
    cronJobs: 'Cron Jobs',
    addCronJob: '+ Add Job',
    cronNamePlaceholder: 'Job name',
    cronSchedulePlaceholder: 'Schedule (e.g. hourly, daily, 0 9 * * *)',
    cronPromptPlaceholder: 'Agent prompt (optional)',
    cronEmpty: 'No scheduled jobs. Add one above or they are created automatically at startup.',
    enable: 'Enable',
    disable: 'Disable',
    runNow: 'Run Now',
    extraAllowedCommands: 'Extra allowed commands (comma-separated, active in the supervised tier)',
    extraAllowedCommandsPlaceholder: 'e.g. python3, docker, make',
    
    
    
    
    noAllowlist: 'No permanently allowed tools. Tools are approved via the approval gate during supervised mode.',
    
    version: 'Version',
    toolsRegistered: 'Tools registered',
    agentsAvailable: 'Agents available',
    checkUpdates: 'Check for updates',
    checkingUpdates: 'Checking...',
    updateAvailable: 'Update available',
    upToDate: 'Up to date',
    updateUnsupported: 'Not supported',
    latestVersion: 'Latest',
    remove: 'Remove',
    
    
    appearance: 'Appearance',
    soul: 'SOUL & Identity',
    channels: 'Channels',
    extensions: 'Extensions',
    
    
    companion: 'Companion',
    test: 'Test',
    testConnectionOk: 'Connection OK',
    testConnectionFailed: 'Connection failed',
    // Theme
    darkLabel: 'Dark',
    darkDesc: 'Easier on the eyes in low light',
    lightLabel: 'Light',
    lightDesc: 'Clean, bright appearance',
    language: 'Language',
    languageDesc: 'Choose your preferred language for the interface',
    systemLabel: 'System',
    systemDesc: 'Matches your OS setting',
    // Config sub-sections
    agentLimits: 'Agent Limits',
    agentLimitsDesc: 'Operational limits for the agent loop.',
    maxToolRounds: 'Max tool rounds',
    toolTimeout: 'Tool timeout',
    security: 'Security',
    securityDesc: 'Command classification and risk settings.',
    blockHighRisk: 'Block high-risk commands',
    blockHighRiskDesc: 'Commands like shutdown, mkfs, dd, sudo are always blocked',
    requireMediumApproval: 'Require approval for medium-risk',
    requireMediumApprovalDesc: 'curl, apt, pip, git require user approval in supervised mode',
    shellTool: 'Shell Tool',
    shellToolDesc: 'Shell execution limits.',
    maxOutputBytes: 'Max output bytes',
    memoryPipeline: 'Memory Pipeline',
    memoryPipelineDesc: 'Background processing parameters.',
    workerCount: 'Worker count',
    treeBucketSize: 'Tree bucket size',
    archiveMsgLimit: 'Archive msg limit',
    freshnessHalfLife: 'Freshness half-life',
    retrievalWeights: 'Retrieval Weights',
    retrievalWeightsDesc: 'Signal weights for multi-strategy search. Must sum to 1.0.',
    sum: 'Sum',
    circuitBreaker: 'Circuit Breaker',
    circuitBreakerDesc: 'Failure thresholds that stop the agent to prevent infinite loops.',
    repeatFailures: 'Repeat failures (same tool+args)',
    noProgressFails: 'No-progress failures (consecutive)',
    costBudget: 'Cost Budget',
    costBudgetDesc: 'Monthly API cost budget for tracking.',
    monthlyBudget: 'Monthly budget',
    dataDir: 'Workspace',
    dataDirDesc: 'Root directory for all data: config, database, extensions, logs, and screenshots. Requires restart after changing.',
    dataDirPlaceholder: 'Default: ~/.mneme',
    dataDirHint: 'All files will be migrated from the old workspace to the new one.',
    // Provider settings
    addProvider: 'Add Provider',
    defaultProvider: 'Default provider',
    defaultProviderDesc: 'Select which provider to use for chat',
    
    
    modelRouting: 'Model routing',
    modelRoutingCoding: 'Coding',
    modelRoutingReasoning: 'Reasoning',
    modelRoutingSummary: 'Summary',
    modelRoutingVision: 'Vision',
    useDefault: 'use default',
    noProviders: 'No providers configured.',
    noProvidersHint: 'Add one to get started, or set API keys via environment variables.',
    edit: 'Edit',
    name: 'Name',
    type: 'Type',
    apiKey: 'API Key',
    baseUrl: 'Base URL',
    models: 'Models',
    addModel: 'Add',
    saving: 'Saving...',
    save: 'Save',
    update: 'Update',
    cancel: 'Cancel',
    editProvider: 'Edit {name}',
    addNewProvider: 'Add Provider',
    modelPlaceholder: 'e.g. gpt-4o',
    // Prompt settings
    promptTemplates: 'Prompt Templates',
    selectPrompt: 'Select a prompt template to edit',
    viewDefault: 'View default',
    hideDefault: 'Hide default',
    originalDefault: 'Original default',
    delete: 'Delete',
    customizedHint: 'Orange dot = customized',
    reset: 'Reset',
    resetToDefault: 'Reset to default',
    chars: '{count} chars',
    charsDefault: '{count} chars (default)',
    newPrompt: '+ New',
    customBadge: 'custom',
    promptNamePlaceholder: 'my_prompt',
    promptBodyPlaceholder: 'Prompt template text...',
    defaultUnavailable: 'Not available — rebuild the Go binary if this persists.',
    providerNamePlaceholder: 'e.g. openai',
    // Prompt template labels — shown in the prompt editor sidebar.
    promptLabels: {
      system_identity: 'System — Identity',
      system_safety: 'System — Safety Rules',
      system_guidance: 'System — Response Guidelines',
      agent_general: 'Agent — General Assistant',
      agent_coder: 'Agent — Coding Specialist',
      agent_archivist: 'Agent — Memory Archivist',
      agent_orchestrator: 'Agent — Orchestrator',
      agent_researcher: 'Agent — Researcher',
      agent_critic: 'Agent — Critic',
      agent_planner: 'Agent — Planner',
      agent_summarizer: 'Agent — Summarizer',
      agent_help: 'Agent — Help',
      agent_tools_agent: 'Agent — Tools Specialist',
      agent_trigger_reactor: 'Agent — Trigger Reactor',
      summarizer_tool_output: 'Summarizer — Tool Output',
      archivist_memory: 'Archivist — Memory Curation',
      council_chair: 'Model Council — Chair Synthesis',
      nudge_memory_gaps: 'Nudge — Memory Gaps',
      nudge_idle: 'Nudge — Idle Reminder'
    }
  },

  dashboard: {
    title: 'Dashboard',
    threads: 'Threads',
    tools: 'Tools',
    agents: 'Agents',
    pending: 'Pending',
    
    
    
    
    
    failedToLoad: 'Failed to load dashboard data',
    retry: 'Retry',
    refresh: 'Refresh',
    
    reflections: 'Subconscious Reflections',
    preferences: 'Preferences',
    reflection: 'reflection'
  },

  monitor: {
    title: 'Background Commands',
    loading: 'Loading...',
    refresh: 'Refresh',
    command: 'Command',
    commandPlaceholder: 'e.g. npm run build',
    timeout: 'Timeout (s)',
    start: 'Start',
    stop: 'Stop',
    output: 'Output',
    hide: 'Hide',
    exitCode: 'exit: {code}',
    emptyHint: 'No monitor runs. Commands started by agents appear here.',
    statusRunning: 'running',
    statusCompleted: 'completed',
    statusFailed: 'failed',
    statusTimeout: 'timeout',
    statusUnknown: 'unknown'
  },

  memory: {
    title: 'Memory Search',
    searchPlaceholder: 'Search your memory...',
    search: 'Search',
    searching: 'Searching...',
    emptyHint: 'Search across all your stored memories',
    emptySub: 'Conversations, facts, entities, and relationships',
    filterAll: 'All',
    filterAllDesc: 'Combined FTS5, vector, and graph search',
    filterFTS5: 'FTS5',
    filterFTS5Desc: 'Full-text BM25 search over memory content',
    filterVector: 'Vector',
    filterVectorDesc: 'Semantic similarity search using embeddings',
    filterGraph: 'Graph',
    filterGraphDesc: 'Knowledge graph entity co-occurrence search',
    resultsFor: 'Results for',
    noResults: 'No results found',
    config: 'Memory Configuration',
    weights: 'Retrieval Weights',
    profile: 'Profile',
    profileBalanced: 'Balanced',
    profileSemantic: 'Semantic',
    profileLexical: 'Lexical',
    profileGraphFirst: 'Graph First'
  },

  todo: {
    title: 'Task board',
    addPlaceholder: 'Add task...',
    add: 'Add'
  },

  capabilities: {
    title: 'Capabilities',
    installServer: 'Install MCP Server',
    installTitle: 'Install MCP Server',
    name: 'Server Name',
    transport: 'Transport',
    command: 'Command',
    args: 'Args (comma-separated)',
    url: 'URL',
    installConnect: 'Install & Connect',
    installing: 'Installing...',
    noSets: 'No capability sets found.',
    noDescription: 'No description',
    noTools: 'No tools',
    disconnect: 'Disconnect',
    connect: 'Connect',
    remove: 'Remove',
    cancel: 'Cancel',
    sideEffects: 'side effects',
    concurrent: 'concurrent',
    hidden: 'hidden',
    background: 'bg',
    maxIter: 'max {count} iter',
    // Capability kind: singular badge label and plural filter-tab label.
    kindBuiltin: 'Built-in',
    kindExtension: 'Extension',
    kindMcp: 'MCP',
    kindSkill: 'Skill',
    groupExtensions: 'Extensions',
    groupMcpServers: 'MCP Servers',
    groupSkills: 'Skills',
    tools: 'Tools',
    agents: 'Agents',
    toolsCount: '{count} tools',
    agentsCount: '{count} agents',
    moreCount: '+{count} more',
    addAgent: '+ Agent',
    allTab: 'All ({count})',
    summary: '{tools} tools · {agents} agents · {sources} sources',
    extensionsManaged: 'Extensions are managed in Settings → Extensions.',
    skillInstallHint: 'Install a skill from a URL (GitHub raw, any HTTP URL) or a local SKILL.md path.',
    skillUrlPlaceholder: 'https://raw.githubusercontent.com/org/repo/main/SKILL.md',
    skillInstalled: 'Skill installed. It will appear in the list after the next refresh.',
    createAgent: 'Create Agent',
    createAgentBtn: 'Create',
    editAgent: 'Edit {name}',
    editAgentTitle: 'Edit agent',
    labelId: 'ID',
    labelDescription: 'Description',
    labelTier: 'Tier',
    labelModelOptional: 'Model (optional)',
    labelMaxIterations: 'Max Iterations',
    labelToolAllowlist: 'Tool Allowlist (comma-separated, or * for all)',
    labelSystemPrompt: 'System Prompt',
    idPlaceholder: 'my_agent',
    namePlaceholder: 'My Agent',
    descriptionPlaceholder: 'What this agent does...',
    modelPlaceholder: 'inherit',
    toolAllowlistPlaceholder: 'read_file, write_file, shell',
    systemPromptPlaceholder: 'You are a helpful assistant...',
    deleteAgentConfirm: 'Delete agent "{id}"?',
    serverNamePlaceholder: 'my-server',
    commandPlaceholder: 'npx -y @modelcontextprotocol/server-filesystem',
    argsPlaceholder: '/path/to/allowed',
    urlPlaceholder: 'http://localhost:8080'
  },

  errors: {
    somethingWrong: 'Something went wrong',
    chatTurnFailed: 'Chat turn failed',
    unexpected: 'An unexpected error occurred.',
    loadTasks: 'Failed to load tasks',
    addTask: 'Failed to add task',
    updateTask: 'Failed to update task',
    removeTask: 'Failed to remove task',
    loadCapabilities: 'Failed to load capabilities',
    loadChannels: 'Failed to load channels',
    connectFailed: 'Connect failed',
    disconnectFailed: 'Disconnect failed',
    removeFailed: 'Remove failed',
    installFailed: 'Install failed',
    uninstallFailed: 'Uninstall failed',
    agentSaveFailed: 'Agent save failed',
    agentDeleteFailed: 'Agent delete failed',
    loadNotifications: 'Failed to load notifications',
    markAllReadFailed: 'Failed to mark all read',
    markReadFailed: 'Failed to mark notification read',
    createThreadFailed: 'Failed to create thread',
    deleteThreadFailed: 'Failed to delete thread',
    invalidResponse: 'Invalid response from server',
    loadMonitorRuns: 'Failed to load monitor runs',
    startFailed: 'Start failed',
    tryAgain: 'Try again'
  },

  companion: {
    title: 'Desktop Companion',
    description: 'The desktop companion captures your screen, listens to your voice (STT), sends the context to the AI provider, and speaks the response (TTS).',
    activate: 'Activate Companion',
    activating: 'Activating...',
    
    
    startLoop: 'Start Loop',
    stopLoop: 'Stop Loop',
    stopped: 'Companion stopped',
    error: 'Error',
    screenIntel: 'Screen Intelligence',
    screenIntelDesc: 'Periodic screen analysis at a configurable interval. Requires a vision-capable model configured in Model Routes.',
    intervalLabel: 'Interval (seconds):',
    startPeriodic: 'Start Periodic',
    startScreenIntel: 'Start Screen Intel',
    stopScreenIntel: 'Stop Screen Intel',
    requirements: 'Requirements',
    reqProvider: 'AI provider configured (OpenAI or Anthropic with vision support)',
    reqMic: 'Microphone access for voice input',
    reqMacCliclick: 'macOS: cliclick (brew install cliclick) for mouse control',
    reqMacPerm: 'macOS: System Events permission in Accessibility settings',
    reqLinux: 'Linux: xdotool (X11) or ydotool (Wayland)'
  },
  voice: {
    title: 'Voice & Speech',
    
    
    
    sttSection: 'Speech-to-Text',
    ttsSection: 'Text-to-Speech',
    engine: 'Engine',
    model: 'Model',
    voice: 'Voice',
    sttShort: 'STT',
    ttsShort: 'TTS',
    endpoint: 'API Endpoint',
    modelVoice: 'Model:Voice (e.g. tts-1:alloy)',
    sttSystem: 'System (auto-detect)',
    sttWhisper: 'Whisper (local whisper.cpp)',
    sttOpenAI: 'OpenAI (cloud API)',
    apiKey: 'API Key',
    apiKeyHint: 'Independent from AI provider keys. Stored in config.toml [voice] section.',
    ttsSystem: 'System (OS native)',
    ttsPiper: 'Piper (local)',
    ttsOpenAI: 'OpenAI (cloud API)'
  },

  keyring: {
    title: 'Keyring',
    status: 'Keyring Status',
    available: 'Available',
    unavailable: 'OS keyring is unavailable',
    mode: 'Storage Mode',
    backend: 'Backend',
    consentTitle: 'Storage Consent',
    consentBody: 'The OS keyring is not available. Store secrets locally?',
    consentAllow: 'Allow Local Storage',
    consentDecline: 'Decline',
    retryProbe: 'Retry'
  },

  home: {
    welcome: 'Welcome to Mneme',
    tagline: 'Your AI assistant for managing conversations, tools, and memory.',
    
    newChat: 'New Chat',
    newChatDesc: 'Start a new conversation with an AI agent',
    searchMemory: 'Search Memory',
    searchMemoryDesc: 'Search across conversations, facts, and knowledge',
    manageTools: 'Capabilities',
    manageToolsDesc: 'Manage tools, MCP servers, agents, and extensions',
    systemStatus: 'System Status',
    providerConnected: 'Provider',
    connected: 'Connected',
    notConnected: 'Not Connected',
    
    
    dbStatus: 'Database'
  },

  webhooks: {
    title: 'Webhooks',
    
    addTunnel: 'Add Tunnel',
    newTunnel: 'New Tunnel',
    target: 'Target',
    targetId: 'Target ID',
    targetEcho: 'Echo (debug)',
    targetAgent: 'Agent',
    description: 'Description',
    descPlaceholder: 'e.g. GitHub push notifications',
    noTunnels: 'No tunnels configured. Add one to receive webhooks from external services.',
    helpText: 'Tunnels route incoming webhook requests to agents. Each tunnel has a unique UUID. External services POST to /webhook with the X-Tunnel-UUID header.',
    copy: 'Copy',
    copied: 'Copied!',
    deleted: 'Tunnel removed.',
    refreshActivity: 'Refresh Activity',
    showActivity: 'Show Recent Activity',
    clear: 'Clear',
    noActivity: 'No recent activity.'
  },

  autonomy: {
    title: 'Autonomy',
    level: 'Autonomy Level',
    tierReadonlyDesc: 'Read-only tools; no writes or command execution',
    tierSupervisedDesc: 'Risky actions require approval',
    tierFullDesc: 'Unrestricted; approval gate bypassed',
    workspaceOnly: 'Workspace Only'
  },

  soul: {
    
    soulTitle: 'SOUL.md',
    soulDesc: 'Defines the agent\'s core identity, operating principles, and behavioral guidelines. This is injected into every agent prompt.',
    identityTitle: 'IDENTITY.md',
    identityDesc: 'Stores user-specific context — name, preferences, active projects, and other details the agent should remember across conversations.',
    save: 'Save',
    saving: 'Saving…',
    saved: 'Saved',
    error: 'Error',
    tip: 'Tip',
    tipText: 'SOUL.md defines <em>who</em> the agent is. IDENTITY.md defines <em>what the agent knows about you</em>.',
    tipChanges: 'Changes take effect on the next conversation turn — no restart needed.'
  },

  diagnostics: {
    title: 'Diagnostics',
    totalTools: 'Total Tools',
    enabledTools: 'Enabled Tools',
    writeSurfaces: 'Write Surfaces',
    recentDenials: 'Recent Denials',
    sideEffectsSub: 'tools with side effects',
    providerReady: 'Ready',
    providerNotConfigured: 'Not configured',
    notAvailable: 'Not available',
    inProcess: 'In-process',
    mcpStdio: 'MCP stdio',
    mcpHttp: 'MCP http',
    jsonRpc: 'JSON-RPC',
    bySource: 'By source:',
    healthChecks: 'Health Checks',
    cost: 'Cost',
    totalCost: 'Total cost',
    budgetUsed: 'Budget used'
  },

  channels: {
    
    configured: 'Configured Channels',
    available: 'Available Channel Types',
    noChannels: 'No channels configured. Add channel credentials in config to enable messaging integrations.',
    configHint: 'Configure these in',
    under: 'under',
    extensionsNote: 'Extensions can register additional channel types via the Provider interface.',
    enabled: 'Enabled',
    disabled: 'Disabled',
    enable: 'Enable',
    disable: 'Disable',
    add: 'Add',
    addHint: 'Add a channel to create its config entry and start it at runtime.',
    running: 'Running',
    notRunning: 'Not running'
  },

  extensions: {
    title: 'Extensions',
    installHint: 'Extensions add tools, agents, channels, and integrations via the extension protocol.',
    installPlaceholder: 'Path to extension directory (containing manifest.json)',
    install: 'Install',
    installing: 'Installing...',
    installed: 'Installed Extensions',
    noExtensions: 'No extensions installed. Install one using the path input above.',
    remove: 'Remove',
    loaded: 'Loaded',
    notLoaded: 'Not loaded',
    formatHint: 'Each extension is a directory with a manifest.json and a binary (or script) that speaks JSON-RPC 2.0 over stdin/stdout.',
    autoBuildHint: 'Extensions are discovered automatically from extensions/ at startup.',
    homepage: 'Homepage',
    uninstallConfirm: 'Uninstall {name}?'
  },

  

  notifications: {
    title: 'Notifications',
    all: 'All',
    unread: 'Unread',
    empty: 'No notifications yet',
    noUnread: 'No unread notifications',
    markAllRead: 'Mark all read',
    approvalRequest: 'Approval Request',
    untitled: 'Notification'
  },

  onboarding: {
    welcomeTitle: 'Welcome to Mneme',
    welcomeDesc: 'Your AI assistant that can manage conversations, execute tools, search memory, and automate tasks — all from your desktop.',
    providersTitle: 'Connect AI Providers',
    providersDesc: 'Mneme works with OpenAI, Anthropic, and local models via Ollama. You can configure providers now or later in Settings.',
    
    providersConfigured: '{count} provider(s) configured',
    
    workspaceTitle: 'Your Workspace',
    workspaceDesc: 'The agent works within your workspace directory. Files, conversations, and memory are stored here.',
    workspacePath: 'Workspace Directory',
    workspacePlaceholder: '<exe_dir>/data',
    readyTitle: "You're All Set!",
    readyDesc: 'Start a conversation, ask the agent to help with a task, or explore the Settings to customize your experience.',
    readyReminder: 'Tip: You can add AI providers anytime in Settings → Providers.',
    skillsTitle: 'Built-in Capabilities',
    skillsDesc: 'Mneme comes with powerful built-in tools ready to use out of the box.',
    skillsMemory: 'Search and recall past conversations, facts, and knowledge.',
    skillsTools: 'File operations, shell commands, web search, and more.',
    skillsApprovals: 'Supervised mode prompts for approval before sensitive actions.',
    skillsDashboard: 'Track usage, costs, and system health.',
    voiceTitle: 'Voice & Speech',
    voiceDesc: 'Mneme supports speech-to-text and text-to-speech for hands-free interaction.',
    voiceStt: 'Speech-to-Text',
    voiceSttDesc: 'Dictate messages using your microphone. Supports system STT, Whisper, and Vosk.',
    voiceTts: 'Text-to-Speech',
    voiceTtsDesc: 'Have responses read aloud. Supports system TTS and Piper.',

    providerAdded: 'Provider added — you can configure more in Settings at any time.',
    providerType: 'Provider type',
    providerTypeOpenAI: 'OpenAI-compatible (OpenAI, Groq, DeepSeek, etc.)',
    providerTypeAnthropic: 'Anthropic-compatible (Claude, Amazon Bedrock, etc.)',
    providerTypeOllama: 'Ollama (local models — Llama, Mistral, Gemma, etc.)',
    baseUrlRequired: 'Base URL is required',
    apiKeyRequired: 'API key is required for this provider type',
    baseUrl: 'Base URL',
    required: 'required',
    apiKey: 'API key',
    notNeeded: 'not needed for local models',
    models: 'Models',
    commaSeparated: 'comma-separated',
    addProviderBtn: 'Add Provider',
    adding: 'Adding...',
    apply: 'Apply',
    applying: 'Applying...',
    restartNote: 'Requires restart after changing',
    workspaceNote: 'Stores conversations, memory, extensions, and configuration.',
    workspaceChangeLater: 'You can change this later in Settings → General.',
    workspaceUpdated: 'Workspace updated. Restart required.',
    applyFailed: 'Apply failed: {error}',
    voiceNote: 'Voice features can be configured in Settings at any time.',
    skip: 'Skip',
    back: 'Back',
    next: 'Next',
    getStarted: 'Get Started'
  },

  walkthrough: {
    step: 'Step {current} of {total}',
    skip: 'Skip',
    back: 'Back',
    next: 'Next',
    gotIt: 'Got it',
    steps: {
      chat: {
        title: 'Chat',
        content: 'Start conversations with AI agents here. Ask questions, give tasks, or browse your threads.'
      },
      memory: {
        title: 'Memory',
        content: 'Search and explore your AI\'s memory. View knowledge graphs, timelines, and entity associations.'
      },
      capabilities: {
        title: 'Capabilities',
        content: 'Browse all tools, agents, MCP servers, and extensions in one place. Install new MCP servers or manage extensions.'
      },
      settings: {
        title: 'Settings',
        content: 'Configure providers, autonomy levels, appearance, and more.'
      }
    }
  }
};

export default en;
export type EnTranslations = typeof en;
