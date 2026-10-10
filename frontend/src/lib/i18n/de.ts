import type { EnTranslations } from './en';
import type { DeepPartial } from './I18nContext';

const de: DeepPartial<EnTranslations> = {
  app: { title: 'Mneme' },

  nav: {
    home: 'Startseite',
    chat: 'Chat',
    memory: 'Speicher',
    tools: 'Werkzeuge',
    dashboard: 'Dashboard',
    notifications: 'Benachrichtigungen',
    settings: 'Einstellungen'
  },

  sidebar: {
    threads: 'Threads',
    newThread: 'Neuer Thread',
    searchThreads: 'Threads durchsuchen...',
    noThreads: 'Noch keine Threads',
    noMatch: 'Keine passenden Threads',
    msgs: 'Nachr.',
    closeSidebar: 'Seitenleiste schließen',
    deleteThread: 'Thread löschen',
    count: '{count} Threads',
    openSidebar: 'Seitenleiste öffnen'
  },

  chat: {
    newConversation: 'Neue Unterhaltung',
    selectThread: 'Wähle einen Thread oder erstelle einen neuen, um zu beginnen.',
    startConversation: 'Unterhaltung starten',
    startHint: 'Sende eine Nachricht zum Beginnen',
    placeholder: 'Nachricht senden... (Enter zum Senden, Shift+Enter für neue Zeile)',
    
    
    
    you: 'Du',
    assistant: 'Mneme',
    toolResult: 'Werkzeug-Ergebnis',
    tool: 'Tool',
    copy: 'Copy',
    copied: 'Copied!',
    thinkingShow: 'Show thinking',
    thinkingHide: 'Hide thinking',
    delete: 'Löschen',
    messages: '{count} Nachrichten'
  },

  approval: {
    required: 'Genehmigung erforderlich',
    wantsToExecute: 'möchte ausführen',
    approveOnce: 'Einmal genehmigen',
    alwaysAllow: 'Immer erlauben',
    deny: 'Ablehnen',
    
    
    
    
    pending: 'ausstehend'
  },

  settings: {
    title: 'Einstellungen',
    general: 'Allgemein',
    providers: 'Anbieter',
    
    approvals: 'Genehmigungen',
    theme: 'Design',
    config: 'Konfiguration',
    prompts: 'Prompts',
    about: 'Über',
    systemHealth: 'Systemzustand',
    cronJobs: 'Cron-Aufträge',
    
    
    
    
    noAllowlist: 'Keine dauerhaft erlaubten Werkzeuge.',
    
    version: 'Version',
    toolsRegistered: 'Registrierte Werkzeuge',
    agentsAvailable: 'Verfügbare Agenten',
    checkUpdates: 'Auf Updates prüfen',
    remove: 'Entfernen',
    appearance: 'Erscheinungsbild',
    soul: 'SOUL & Identität',
    channels: 'Kanäle',
    extensions: 'Erweiterungen',
    
    
    
    
    darkLabel: 'Dunkel',
    darkDesc: 'Schonender für die Augen bei wenig Licht',
    lightLabel: 'Hell',
    lightDesc: 'Klares, helles Erscheinungsbild',
    systemLabel: 'System',
    systemDesc: 'Passt sich der OS-Einstellung an',
    agentLimits: 'Agent-Limits',
    agentLimitsDesc: 'Betriebsgrenzen für die Agent-Schleife.',
    maxToolRounds: 'Max. Werkzeug-Runden',
    toolTimeout: 'Werkzeug-Timeout',
    maxHistoryMessages: 'Max. Verlaufsnachrichten',
    security: 'Sicherheit',
    securityDesc: 'Befehlsklassifizierung und Risikoeinstellungen.',
    blockHighRisk: 'Hochrisiko-Befehle blockieren',
    blockHighRiskDesc: 'Befehle wie shutdown, mkfs, dd, sudo sind immer blockiert',
    requireMediumApproval: 'Genehmigung für mittleres Risiko',
    requireMediumApprovalDesc: 'curl, apt, pip, git benötigen Genehmigung im überwachten Modus',
    shellTool: 'Shell-Werkzeug',
    shellToolDesc: 'Ausführungslimits der Shell.',
    maxOutputBytes: 'Max. Ausgabebytes',
    memoryPipeline: 'Speicher-Pipeline',
    memoryPipelineDesc: 'Hintergrundverarbeitungsparameter.',
    workerCount: 'Worker-Anzahl',
    treeBucketSize: 'Baum-Bucket-Größe',
    archiveMsgLimit: 'Archiv-Nachrichtenlimit',
    freshnessHalfLife: 'Aktualitäts-Halbwertszeit',
    retrievalWeights: 'Abruf-Gewichtungen',
    retrievalWeightsDesc: 'Signalgewichte für Multi-Strategie-Suche. Müssen 1,0 ergeben.',
    sum: 'Summe',
    circuitBreaker: 'Leistungsschutzschalter',
    circuitBreakerDesc: 'Fehlerschwellen, die den Agenten stoppen, um Endlosschleifen zu verhindern.',
    repeatFailures: 'Wiederholungsfehler (gleiches Werkzeug+Args)',
    noProgressFails: 'Fortschrittslose Fehler (aufeinanderfolgend)',
    hardRejects: 'Harte Ablehnungen (Richtlinienblockaden)',
    costBudget: 'Kostenbudget',
    costBudgetDesc: 'Monatliches API-Kostenbudget zur Verfolgung.',
    monthlyBudget: 'Monatsbudget',
    dataDir: 'Datenverzeichnis',
    dataDirDesc: 'Wo Datenbank und Laufzeitdaten gespeichert werden.',
    dataDirPlaceholder: 'Standard: <workspace>/data',
    dataDirHint: 'Leer lassen für den Standardort im Workspace-Verzeichnis.',
    addProvider: 'Anbieter hinzufügen',
    defaultProvider: 'Standardanbieter',
    defaultProviderDesc: 'Wählen Sie den Anbieter für den Chat',
    modelRouting: 'Modell-Routing',
    modelRoutingCoding: 'Programmierung',
    modelRoutingReasoning: 'Analyse',
    modelRoutingSummary: 'Zusammenfassung',
    modelRoutingVision: 'Vision',
    useDefault: 'Standard',
    
    
    noProviders: 'Keine Anbieter konfiguriert.',
    noProvidersHint: 'Füge einen hinzu oder setze API-Schlüssel via Umgebungsvariablen.',
    edit: 'Bearbeiten',
    name: 'Name',
    type: 'Typ',
    apiKey: 'API-Schlüssel',
    baseUrl: 'Basis-URL',
    models: 'Modelle',
    addModel: 'Hinzufügen',
    saving: 'Speichern...',
    save: 'Speichern',
    update: 'Aktualisieren',
    cancel: 'Abbrechen',
    editProvider: '{name} bearbeiten',
    addNewProvider: 'Anbieter hinzufügen',
    promptTemplates: 'Prompt-Vorlagen',
    viewDefault: 'Original anzeigen',
    hideDefault: 'Original ausblenden',
    originalDefault: 'Original',
    delete: 'Löschen',
    selectPrompt: 'Wähle eine Prompt-Vorlage zum Bearbeiten',
    customizedHint: 'Oranger Punkt = angepasst',
    reset: 'Zurücksetzen',
    resetToDefault: 'Auf Standard zurücksetzen',
    chars: '{count} Zeichen',
    charsDefault: '{count} Zeichen (Standard)',
    promptLabels: {
      system_identity: 'System — Identität',
      system_safety: 'System — Sicherheitsregeln',
      system_guidance: 'System — Antwortrichtlinien',
      agent_general: 'Agent — Allgemeiner Assistent',
      agent_coder: 'Agent — Code-Spezialist',
      agent_archivist: 'Agent — Speicher-Archivar',
      agent_orchestrator: 'Agent — Orchestrator',
      agent_researcher: 'Agent — Rechercheur',
      agent_critic: 'Agent — Kritiker',
      agent_planner: 'Agent — Planer',
      agent_summarizer: 'Agent — Zusammenfasser',
      agent_help: 'Agent — Hilfe',
      agent_tools_agent: 'Agent — Werkzeugspezialist',
      agent_trigger_reactor: 'Agent — Trigger-Reaktor',
      summarizer_tool_output: 'Zusammenfasser — Werkzeugausgabe',
      archivist_memory: 'Archivar — Speicherkuratierung',
      council_chair: 'Modell-Rat — Vorsitz-Synthese',
      nudge_memory_gaps: 'Anstoß — Speicherlücken',
      nudge_idle: 'Anstoß — Inaktivitätserinnerung'
    }
  },

  dashboard: {
    title: 'Dashboard',
    threads: 'Threads',
    tools: 'Werkzeuge',
    agents: 'Agenten',
    pending: 'Ausstehend',
    
    
    
    
    
    failedToLoad: 'Dashboard-Daten konnten nicht geladen werden',
    retry: 'Wiederholen',
    refresh: 'Aktualisieren',
    
    reflections: 'Unterbewusste Reflexionen',
    preferences: 'Preferences'
  },

  memory: {
    title: 'Speichersuche',
    searchPlaceholder: 'Durchsuche deine Erinnerungen...',
    search: 'Suchen',
    searching: 'Suche...',
    emptyHint: 'Durchsuche alle gespeicherten Erinnerungen',
    emptySub: 'Unterhaltungen, Fakten, Entitäten und Beziehungen',
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
    profile: 'Profil'
  },

  todo: {
    title: 'Aufgabenboard',
    addPlaceholder: 'Aufgabe hinzufügen...',
    add: 'Hinzufügen'
  },

  errors: {
    somethingWrong: 'Etwas ist schiefgelaufen',
    
    
    
    tryAgain: 'Erneut versuchen'
  },
  keyring: {
    title: 'Keyring',
    consentTitle: 'Storage Consent',
    consentBody: 'The OS keyring is not available. Store secrets locally?',
    consentAllow: 'Allow',
    consentDecline: 'Decline'
  },

  home: {
    welcome: 'Willkommen bei Mneme',
    tagline: 'Ihr KI-Assistent für die Verwaltung von Konversationen, Werkzeugen und Speicher.',
    
    newChat: 'Neuer Chat',
    newChatDesc: 'Eine neue Konversation mit einem KI-Agenten starten',
    searchMemory: 'Speicher durchsuchen',
    searchMemoryDesc: 'Konversationen, Fakten und Wissen durchsuchen',
    manageTools: 'Werkzeuge verwalten',
    manageToolsDesc: 'Werkzeuge, Agenten und MCP-Server konfigurieren',
    systemStatus: 'Systemstatus',
    providerConnected: 'Anbieter',
    connected: 'Verbunden',
    notConnected: 'Nicht verbunden',
    
    
    dbStatus: 'Datenbank'
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

export default de;