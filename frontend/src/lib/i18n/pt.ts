import type { EnTranslations } from './en';
import type { DeepPartial } from './I18nContext';

const pt: DeepPartial<EnTranslations> = {
  app: { title: 'Mneme' },

  nav: {
    home: 'Início',
    chat: 'Chat',
    memory: 'Memória',
    tools: 'Ferramentas',
    dashboard: 'Painel',
    notifications: 'Notificações',
    settings: 'Configurações'
  },

  sidebar: {
    threads: 'Tópicos',
    newThread: 'Novo tópico',
    searchThreads: 'Buscar tópicos...',
    noThreads: 'Nenhum tópico ainda',
    noMatch: 'Nenhum tópico correspondente',
    msgs: 'msgs',
    closeSidebar: 'Fechar barra',
    deleteThread: 'Excluir tópico',
    count: '{count} tópicos',
    openSidebar: 'Abrir barra lateral'
  },

  chat: {
    newConversation: 'Nova conversa',
    selectThread: 'Selecione um tópico ou crie um novo para começar.',
    startConversation: 'Iniciar conversa',
    startHint: 'Envie uma mensagem para começar',
    placeholder: 'Enviar mensagem... (Enter para enviar, Shift+Enter para nova linha)',
    
    
    
    you: 'Você',
    assistant: 'Mneme',
    toolResult: 'Resultado da ferramenta',
    tool: 'Tool',
    copy: 'Copy',
    copied: 'Copied!',
    thinkingShow: 'Show thinking',
    thinkingHide: 'Hide thinking',
    delete: 'Excluir',
    messages: '{count} mensagens'
  },

  approval: {
    required: 'Aprovação necessária',
    wantsToExecute: 'quer executar',
    approveOnce: 'Aprovar uma vez',
    alwaysAllow: 'Permitir sempre',
    deny: 'Negar',
    
    
    
    
    pending: 'pendente'
  },

  settings: {
    title: 'Configurações',
    general: 'Geral',
    providers: 'Provedores',
    
    approvals: 'Aprovações',
    theme: 'Tema',
    config: 'Configuração',
    prompts: 'Prompts',
    about: 'Sobre',
    systemHealth: 'Saúde do sistema',
    cronJobs: 'Tarefas Cron',
    
    
    
    
    noAllowlist: 'Nenhuma ferramenta permanentemente permitida.',
    
    version: 'Versão',
    toolsRegistered: 'Ferramentas registradas',
    agentsAvailable: 'Agentes disponíveis',
    checkUpdates: 'Verificar atualizações',
    remove: 'Remover',
    appearance: 'Aparência',
    soul: 'SOUL & Identity',
    channels: 'Channels',
    extensions: 'Extensions',
    
    
    
    
    darkLabel: 'Escuro',
    darkDesc: 'Mais suave para os olhos em baixa luminosidade',
    lightLabel: 'Claro',
    lightDesc: 'Aparência limpa e brilhante',
    systemLabel: 'Sistema',
    systemDesc: 'Acompanha a configuração do SO',
    agentLimits: 'Limites do agente',
    agentLimitsDesc: 'Limites operacionais para o loop do agente.',
    maxToolRounds: 'Máx. rodadas de ferramentas',
    toolTimeout: 'Timeout de ferramenta',
    maxHistoryMessages: 'Máx. mensagens do histórico',
    security: 'Segurança',
    securityDesc: 'Classificação de comandos e configurações de risco.',
    blockHighRisk: 'Bloquear comandos de alto risco',
    blockHighRiskDesc: 'Comandos como shutdown, mkfs, dd, sudo sempre bloqueados',
    requireMediumApproval: 'Exigir aprovação para risco médio',
    requireMediumApprovalDesc: 'curl, apt, pip, git exigem aprovação no modo supervisionado',
    shellTool: 'Ferramenta Shell',
    shellToolDesc: 'Limites de execução do shell.',
    maxOutputBytes: 'Máx. bytes de saída',
    memoryPipeline: 'Pipeline de memória',
    memoryPipelineDesc: 'Parâmetros de processamento em segundo plano.',
    workerCount: 'Número de workers',
    treeBucketSize: 'Tamanho do bucket da árvore',
    archiveMsgLimit: 'Limite de mensagens do arquivo',
    freshnessHalfLife: 'Meia-vida de frescor',
    retrievalWeights: 'Pesos de recuperação',
    retrievalWeightsDesc: 'Pesos de sinal para busca multi-estratégia. Devem somar 1.0.',
    sum: 'Soma',
    circuitBreaker: 'Disjuntor',
    circuitBreakerDesc: 'Limiares de falha que param o agente para evitar loops infinitos.',
    repeatFailures: 'Falhas repetidas (mesma ferramenta+args)',
    noProgressFails: 'Falhas sem progresso (consecutivas)',
    hardRejects: 'Rejeições fortes (bloqueios de política)',
    costBudget: 'Orçamento de custos',
    costBudgetDesc: 'Orçamento mensal de custos de API para acompanhamento.',
    monthlyBudget: 'Orçamento mensal',
    dataDir: 'Diretório de dados',
    dataDirDesc: 'Onde o banco de dados e dados de execução são armazenados.',
    dataDirPlaceholder: 'Padrão: <workspace>/data',
    dataDirHint: 'Deixe vazio para usar o local padrão no diretório workspace.',
    addProvider: 'Adicionar provedor',
    defaultProvider: 'Provedor padrão',
    defaultProviderDesc: 'Selecionar provedor para chat',
    modelRouting: 'Roteamento de modelos',
    modelRoutingCoding: 'Programação',
    modelRoutingReasoning: 'Raciocínio',
    modelRoutingSummary: 'Resumo',
    modelRoutingVision: 'Visão',
    useDefault: 'padrão',
    
    
    noProviders: 'Nenhum provedor configurado.',
    noProvidersHint: 'Adicione um para começar, ou defina chaves API via variáveis de ambiente.',
    edit: 'Editar',
    name: 'Nome',
    type: 'Tipo',
    apiKey: 'Chave API',
    baseUrl: 'URL base',
    models: 'Modelos',
    addModel: 'Adicionar',
    saving: 'Salvando...',
    save: 'Salvar',
    update: 'Atualizar',
    cancel: 'Cancelar',
    editProvider: 'Editar {name}',
    addNewProvider: 'Adicionar provedor',
    promptTemplates: 'Modelos de prompt',
    viewDefault: 'Ver original',
    hideDefault: 'Ocultar original',
    originalDefault: 'Original padrão',
    delete: 'Excluir',
    selectPrompt: 'Selecione um modelo de prompt para editar',
    customizedHint: 'Ponto laranja = personalizado',
    reset: 'Redefinir',
    resetToDefault: 'Redefinir para o padrão',
    chars: '{count} caracteres',
    charsDefault: '{count} caracteres (padrão)',
    promptLabels: {
      system_identity: 'Sistema — Identidade',
      system_safety: 'Sistema — Regras de segurança',
      system_guidance: 'Sistema — Diretrizes de resposta',
      agent_general: 'Agente — Assistente geral',
      agent_coder: 'Agente — Especialista em código',
      agent_archivist: 'Agente — Arquivista de memória',
      agent_orchestrator: 'Agente — Orquestrador',
      agent_researcher: 'Agente — Pesquisador',
      agent_critic: 'Agente — Crítico',
      agent_planner: 'Agente — Planejador',
      agent_summarizer: 'Agente — Resumidor',
      agent_help: 'Agente — Ajuda',
      agent_tools_agent: 'Agente — Especialista em ferramentas',
      agent_trigger_reactor: 'Agente — Reator de gatilhos',
      summarizer_tool_output: 'Resumidor — Saída de ferramentas',
      archivist_memory: 'Arquivista — Curadoria de memória',
      council_chair: 'Conselho de modelos — Síntese do presidente',
      nudge_memory_gaps: 'Estímulo — Lacunas de memória',
      nudge_idle: 'Estímulo — Lembrete de inatividade'
    }
  },

  dashboard: {
    title: 'Painel',
    threads: 'Tópicos',
    tools: 'Ferramentas',
    agents: 'Agentes',
    pending: 'Pendente',
    
    
    
    
    
    failedToLoad: 'Falha ao carregar dados do painel',
    retry: 'Tentar novamente',
    refresh: 'Atualizar',
    
    reflections: 'Reflexões subconscientes',
    preferences: 'Preferences'
  },

  memory: {
    title: 'Busca na memória',
    searchPlaceholder: 'Busque em sua memória...',
    search: 'Buscar',
    searching: 'Buscando...',
    emptyHint: 'Busque em todas as suas memórias armazenadas',
    emptySub: 'Conversas, fatos, entidades e relacionamentos',
    filterAll: 'All',
    filterAllDesc: 'Combined search',
    filterFTS5: 'FTS5',
    filterFTS5Desc: 'Full-text BM25 search',
    filterVector: 'Vector',
    filterVectorDesc: 'Semantic similarity search',
    filterGraph: 'Graph',
    filterGraphDesc: 'Knowledge graph search',
    resultsFor: 'Results for',
    noResults: 'No results found',
    profile: 'Perfil'
  },

  todo: {
    title: 'Quadro de tarefas',
    addPlaceholder: 'Adicionar tarefa...',
    add: 'Adicionar'
  },

  errors: {
    somethingWrong: 'Algo deu errado',
    
    
    
    tryAgain: 'Tentar novamente'
  },
  keyring: {
    title: 'Keyring',
    consentTitle: 'Storage Consent',
    consentBody: 'The OS keyring is not available. Store secrets locally?',
    consentAllow: 'Allow',
    consentDecline: 'Decline'
  },

  home: {
    welcome: 'Bem-vindo ao Mneme',
    tagline: 'Seu assistente IA para gerenciar conversas, ferramentas e memória.',
    
    newChat: 'Novo chat',
    newChatDesc: 'Inicie uma nova conversa com um agente IA',
    searchMemory: 'Buscar na memória',
    searchMemoryDesc: 'Pesquise em conversas, fatos e conhecimentos',
    manageTools: 'Gerenciar ferramentas',
    manageToolsDesc: 'Configure ferramentas, agentes e servidores MCP',
    systemStatus: 'Status do sistema',
    providerConnected: 'Provedor',
    connected: 'Conectado',
    notConnected: 'Não conectado',
    
    
    dbStatus: 'Banco de dados'
  },

  extensions: {
    title: 'Extensions',
    install: 'Install',
  },

  webhooks: {
    title: 'Webhooks'
  },

  autonomy: {
    title: 'Autonomy'
  },

  soul: {
    
    soulTitle: 'SOUL.md',
    soulDesc: 'Defines the agent core identity.',
    identityTitle: 'IDENTITY.md',
    identityDesc: 'Stores user-specific context.',
    save: 'Save', saving: 'Saving…', saved: 'Saved', error: 'Error',
    tip: 'Tip',
    tipText: 'SOUL.md defines who the agent is. IDENTITY.md defines what the agent knows about you.',
    tipChanges: 'Changes take effect on the next conversation turn.'
  },

  diagnostics: {
    title: 'Diagnostics'
  },

  notifications: {
    title: 'Notifications',
    all: 'All',
    unread: 'Unread',
    empty: 'No notifications yet',
    approvalRequest: 'Approval Request'
  },
  onboarding: {
    welcomeTitle: 'Welcome to Mneme',
    welcomeDesc: 'Your AI assistant that can manage conversations, execute tools, search memory, and automate tasks — all from your desktop.',
    providersTitle: 'Connect AI Providers',
    providersDesc: 'Mneme works with OpenAI, Anthropic, and local models via Ollama.',
    
    workspaceTitle: 'Your Workspace',
    workspaceDesc: 'The agent works within your workspace directory.',
    readyTitle: "You're All Set!",
    readyDesc: 'Start a conversation or explore Settings to customize your experience.',
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
        content: "Search and explore your AI's memory. View knowledge graphs, timelines, and entity associations."
      },
      settings: {
        title: 'Settings',
        content: 'Configure providers, autonomy levels, appearance, and more.'
      }
    }
  }
};

export default pt;