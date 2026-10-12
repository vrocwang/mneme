import type { EnTranslations } from './en';
import type { DeepPartial } from './I18nContext';

const it: DeepPartial<EnTranslations> = {
  app: { title: 'Mneme' },

  nav: {
    home: 'Home',
    chat: 'Chat',
    memory: 'Memoria',
    tools: 'Strumenti',
    dashboard: 'Dashboard',
    notifications: 'Notifiche',
    settings: 'Impostazioni'
  },

  sidebar: {
    threads: 'Thread',
    newThread: 'Nuovo thread',
    searchThreads: 'Cerca thread...',
    noThreads: 'Nessun thread',
    noMatch: 'Nessun thread corrispondente',
    msgs: 'msg',
    closeSidebar: 'Chiudi barra',
    deleteThread: 'Elimina thread',
    count: '{count} thread',
    openSidebar: 'Apri barra laterale'
  },

  chat: {
    newConversation: 'Nuova conversazione',
    selectThread: 'Seleziona un thread o creane uno nuovo per iniziare.',
    startConversation: 'Inizia conversazione',
    startHint: 'Invia un messaggio per iniziare',
    placeholder: 'Invia un messaggio... (Invio per inviare, Shift+Invio per nuova riga)',
    
    
    
    you: 'Tu',
    assistant: 'Mneme',
    toolResult: 'Risultato strumento',
    tool: 'Tool',
    copy: 'Copy',
    copied: 'Copied!',
    thinkingShow: 'Show thinking',
    thinkingHide: 'Hide thinking',
    delete: 'Elimina',
    messages: '{count} messaggi'
  },

  approval: {
    required: 'Approvazione richiesta',
    wantsToExecute: 'vuole eseguire',
    approveOnce: 'Approva una volta',
    alwaysAllow: 'Consenti sempre',
    deny: 'Nega',
    
    
    
    
    pending: 'in attesa'
  },

  settings: {
    title: 'Impostazioni',
    general: 'Generale',
    providers: 'Provider',
    
    approvals: 'Approvazioni',
    theme: 'Tema',
    config: 'Configurazione',
    prompts: 'Prompt',
    about: 'Informazioni',
    systemHealth: 'Stato del sistema',
    cronJobs: 'Job Cron',
    
    
    
    
    noAllowlist: 'Nessuno strumento permanentemente consentito.',
    
    version: 'Versione',
    toolsRegistered: 'Strumenti registrati',
    agentsAvailable: 'Agenti disponibili',
    checkUpdates: 'Verifica aggiornamenti',
    remove: 'Rimuovi',
    appearance: 'Aspetto',
    soul: 'SOUL & Identity',
    channels: 'Channels',
    extensions: 'Extensions',
    
    
    
    
    darkLabel: 'Scuro',
    darkDesc: 'Più delicato per gli occhi in condizioni di scarsa illuminazione',
    lightLabel: 'Chiaro',
    lightDesc: 'Aspetto pulito e luminoso',
    systemLabel: 'Sistema',
    systemDesc: 'Segue l\'impostazione del sistema operativo',
    agentLimits: 'Limiti agente',
    agentLimitsDesc: 'Limiti operativi per il ciclo dell\'agente.',
    maxToolRounds: 'Max. round strumenti',
    toolTimeout: 'Timeout strumenti',
    security: 'Sicurezza',
    securityDesc: 'Classificazione comandi e impostazioni di rischio.',
    blockHighRisk: 'Blocca comandi ad alto rischio',
    blockHighRiskDesc: 'Comandi come shutdown, mkfs, dd, sudo sempre bloccati',
    requireMediumApproval: 'Richiedi approvazione per rischio medio',
    requireMediumApprovalDesc: 'curl, apt, pip, git richiedono approvazione in modalità supervisionata',
    shellTool: 'Strumento Shell',
    shellToolDesc: 'Limiti di esecuzione della shell.',
    maxOutputBytes: 'Max. byte in uscita',
    memoryPipeline: 'Pipeline memoria',
    memoryPipelineDesc: 'Parametri di elaborazione in background.',
    workerCount: 'Numero di worker',
    treeBucketSize: 'Dimensione bucket albero',
    archiveMsgLimit: 'Limite messaggi archivio',
    freshnessHalfLife: 'Emivita di freschezza',
    retrievalWeights: 'Pesi di recupero',
    retrievalWeightsDesc: 'Pesi dei segnali per ricerca multi-strategia. Devono sommare a 1.0.',
    sum: 'Somma',
    circuitBreaker: 'Interruttore',
    circuitBreakerDesc: 'Soglie di errore che fermano l\'agente per prevenire cicli infiniti.',
    repeatFailures: 'Errori ripetuti (stesso strumento+args)',
    noProgressFails: 'Errori senza progresso (consecutivi)',
    costBudget: 'Budget costi',
    costBudgetDesc: 'Budget mensile costi API per il monitoraggio.',
    monthlyBudget: 'Budget mensile',
    dataDir: 'Directory dati',
    dataDirDesc: 'Dove sono memorizzati database e dati di runtime.',
    dataDirPlaceholder: 'Predefinito: <workspace>/data',
    dataDirHint: 'Lascia vuoto per usare la posizione predefinita nella directory workspace.',
    addProvider: 'Aggiungi provider',
    defaultProvider: 'Provider predefinito',
    defaultProviderDesc: 'Seleziona il provider per la chat',
    modelRouting: 'Routing dei modelli',
    modelRoutingCoding: 'Programmazione',
    modelRoutingReasoning: 'Ragionamento',
    modelRoutingSummary: 'Riepilogo',
    modelRoutingVision: 'Visione',
    useDefault: 'predefinito',
    
    
    noProviders: 'Nessun provider configurato.',
    noProvidersHint: 'Aggiungine uno per iniziare, o imposta le chiavi API via variabili d\'ambiente.',
    edit: 'Modifica',
    name: 'Nome',
    type: 'Tipo',
    apiKey: 'Chiave API',
    baseUrl: 'URL base',
    models: 'Modelli',
    addModel: 'Aggiungi',
    saving: 'Salvataggio...',
    save: 'Salva',
    update: 'Aggiorna',
    cancel: 'Annulla',
    editProvider: 'Modifica {name}',
    addNewProvider: 'Aggiungi provider',
    promptTemplates: 'Modelli di prompt',
    viewDefault: 'Vedi originale',
    hideDefault: 'Nascondi originale',
    originalDefault: 'Originale predefinito',
    delete: 'Elimina',
    selectPrompt: 'Seleziona un modello di prompt da modificare',
    customizedHint: 'Punto arancione = personalizzato',
    reset: 'Ripristina',
    resetToDefault: 'Ripristina predefinito',
    chars: '{count} caratteri',
    charsDefault: '{count} caratteri (predefinito)',
    promptLabels: {
      system_identity: 'Sistema — Identità',
      system_safety: 'Sistema — Regole di sicurezza',
      system_guidance: 'Sistema — Linee guida risposta',
      agent_general: 'Agente — Assistente generale',
      agent_coder: 'Agente — Specialista codice',
      agent_archivist: 'Agente — Archivista memoria',
      agent_orchestrator: 'Agente — Orchestratore',
      agent_researcher: 'Agente — Ricercatore',
      agent_critic: 'Agente — Critico',
      agent_planner: 'Agente — Pianificatore',
      agent_summarizer: 'Agente — Riepilogatore',
      agent_help: 'Agente — Aiuto',
      agent_tools_agent: 'Agente — Specialista strumenti',
      agent_trigger_reactor: 'Agente — Reattore trigger',
      summarizer_tool_output: 'Riepilogatore — Output strumenti',
      archivist_memory: 'Archivista — Curazione memoria',
      council_chair: 'Consiglio modelli — Sintesi presidente',
      nudge_memory_gaps: 'Spinta — Lacune memoria',
      nudge_idle: 'Spinta — Promemoria inattività'
    }
  },

  dashboard: {
    title: 'Dashboard',
    threads: 'Thread',
    tools: 'Strumenti',
    agents: 'Agenti',
    pending: 'In attesa',
    
    
    
    
    
    failedToLoad: 'Caricamento dati dashboard fallito',
    retry: 'Riprova',
    refresh: 'Aggiorna',
    
    reflections: 'Riflessioni subconsce',
    preferences: 'Preferences'
  },

  memory: {
    title: 'Ricerca memoria',
    searchPlaceholder: 'Cerca nella tua memoria...',
    search: 'Cerca',
    searching: 'Ricerca in corso...',
    emptyHint: 'Cerca tra tutti i tuoi ricordi memorizzati',
    emptySub: 'Conversazioni, fatti, entità e relazioni',
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
    profile: 'Profilo'
  },

  todo: {
    title: 'Bacheca attività',
    addPlaceholder: 'Aggiungi attività...',
    add: 'Aggiungi'
  },

  errors: {
    somethingWrong: 'Qualcosa è andato storto',
    
    
    
    tryAgain: 'Riprova'
  },
  keyring: {
    title: 'Keyring',
    consentTitle: 'Storage Consent',
    consentBody: 'The OS keyring is not available. Store secrets locally?',
    consentAllow: 'Allow',
    consentDecline: 'Decline'
  },

  home: {
    welcome: 'Benvenuto in Mneme',
    tagline: 'Il tuo assistente IA per gestire conversazioni, strumenti e memoria.',
    
    newChat: 'Nuova chat',
    newChatDesc: 'Avvia una nuova conversazione con un agente IA',
    searchMemory: 'Cerca nella memoria',
    searchMemoryDesc: 'Cerca tra conversazioni, fatti e conoscenze',
    manageTools: 'Gestisci strumenti',
    manageToolsDesc: 'Configura strumenti, agenti e server MCP',
    systemStatus: 'Stato del sistema',
    providerConnected: 'Provider',
    connected: 'Connesso',
    notConnected: 'Non connesso',
    
    
    dbStatus: 'Database'
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

export default it;