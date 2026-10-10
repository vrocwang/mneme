import type { EnTranslations } from './en';
import type { DeepPartial } from './I18nContext';

const ru: DeepPartial<EnTranslations> = {
  app: { title: 'Mneme' },

  nav: {
    home: 'Главная',
    chat: 'Чат',
    memory: 'Память',
    tools: 'Инструменты',
    dashboard: 'Панель',
    notifications: 'Уведомления',
    settings: 'Настройки'
  },

  sidebar: {
    threads: 'Треды',
    newThread: 'Новый тред',
    searchThreads: 'Поиск тредов...',
    noThreads: 'Пока нет тредов',
    noMatch: 'Нет подходящих тредов',
    msgs: 'сообщ.',
    closeSidebar: 'Закрыть панель',
    deleteThread: 'Удалить тред',
    count: '{count} тредов',
    openSidebar: 'Открыть боковую панель'
  },

  chat: {
    newConversation: 'Новый разговор',
    selectThread: 'Выберите тред или создайте новый, чтобы начать.',
    startConversation: 'Начать разговор',
    startHint: 'Отправьте сообщение, чтобы начать',
    placeholder: 'Отправить сообщение... (Enter — отправить, Shift+Enter — новая строка)',
    
    
    
    you: 'Вы',
    assistant: 'Mneme',
    toolResult: 'Результат инструмента',
    tool: 'Tool',
    copy: 'Copy',
    copied: 'Copied!',
    thinkingShow: 'Show thinking',
    thinkingHide: 'Hide thinking',
    delete: 'Удалить',
    messages: '{count} сообщений'
  },

  approval: {
    required: 'Требуется одобрение',
    wantsToExecute: 'хочет выполнить',
    approveOnce: 'Одобрить один раз',
    alwaysAllow: 'Всегда разрешать',
    deny: 'Отказать',
    
    
    
    
    pending: 'ожидает'
  },

  settings: {
    title: 'Настройки',
    general: 'Общие',
    providers: 'Провайдеры',
    
    approvals: 'Одобрения',
    theme: 'Тема',
    config: 'Конфигурация',
    prompts: 'Промпты',
    about: 'О программе',
    systemHealth: 'Состояние системы',
    cronJobs: 'Задачи Cron',
    
    
    
    
    noAllowlist: 'Нет постоянно разрешённых инструментов.',
    
    version: 'Версия',
    toolsRegistered: 'Зарегистрировано инструментов',
    agentsAvailable: 'Доступно агентов',
    checkUpdates: 'Проверить обновления',
    remove: 'Удалить',
    appearance: 'Внешний вид',
    soul: 'SOUL & Identity',
    channels: 'Channels',
    extensions: 'Extensions',
    
    
    
    
    darkLabel: 'Тёмная',
    darkDesc: 'Мягче для глаз при слабом освещении',
    lightLabel: 'Светлая',
    lightDesc: 'Чистый, светлый вид',
    systemLabel: 'Системная',
    systemDesc: 'Соответствует настройкам ОС',
    agentLimits: 'Лимиты агента',
    agentLimitsDesc: 'Операционные ограничения для цикла агента.',
    maxToolRounds: 'Макс. раундов инструментов',
    toolTimeout: 'Тайм-аут инструмента',
    maxHistoryMessages: 'Макс. сообщений истории',
    security: 'Безопасность',
    securityDesc: 'Классификация команд и настройки риска.',
    blockHighRisk: 'Блокировать команды высокого риска',
    blockHighRiskDesc: 'Команды shutdown, mkfs, dd, sudo всегда заблокированы',
    requireMediumApproval: 'Требовать одобрение для среднего риска',
    requireMediumApprovalDesc: 'curl, apt, pip, git требуют одобрения в режиме наблюдения',
    shellTool: 'Инструмент оболочки',
    shellToolDesc: 'Ограничения выполнения команд оболочки.',
    maxOutputBytes: 'Макс. байт вывода',
    memoryPipeline: 'Конвейер памяти',
    memoryPipelineDesc: 'Параметры фоновой обработки.',
    workerCount: 'Количество воркеров',
    treeBucketSize: 'Размер корзины дерева',
    archiveMsgLimit: 'Лимит сообщений архива',
    freshnessHalfLife: 'Период полураспада свежести',
    retrievalWeights: 'Веса извлечения',
    retrievalWeightsDesc: 'Веса сигналов для мультистратегического поиска. Должны давать в сумме 1.0.',
    sum: 'Сумма',
    circuitBreaker: 'Предохранитель',
    circuitBreakerDesc: 'Пороги сбоев, которые останавливают агента для предотвращения бесконечных циклов.',
    repeatFailures: 'Повторные сбои (тот же инструмент+аргументы)',
    noProgressFails: 'Сбои без прогресса (последовательные)',
    hardRejects: 'Жёсткие отказы (блокировки политики)',
    costBudget: 'Бюджет расходов',
    costBudgetDesc: 'Месячный бюджет расходов API для отслеживания.',
    monthlyBudget: 'Месячный бюджет',
    dataDir: 'Каталог данных',
    dataDirDesc: 'Где хранятся база данных и данные времени выполнения.',
    dataDirPlaceholder: 'По умолчанию: <workspace>/data',
    dataDirHint: 'Оставьте пустым для использования расположения по умолчанию в каталоге workspace.',
    addProvider: 'Добавить провайдера',
    defaultProvider: 'Провайдер по умолчанию',
    defaultProviderDesc: 'Выберите провайдера для чата',
    modelRouting: 'Маршрутизация моделей',
    modelRoutingCoding: 'Программирование',
    modelRoutingReasoning: 'Рассуждение',
    modelRoutingSummary: 'Сводка',
    modelRoutingVision: 'Видение',
    useDefault: 'по умолчанию',
    
    
    noProviders: 'Нет настроенных провайдеров.',
    noProvidersHint: 'Добавьте провайдера или задайте ключи API через переменные окружения.',
    edit: 'Изменить',
    name: 'Имя',
    type: 'Тип',
    apiKey: 'Ключ API',
    baseUrl: 'Базовый URL',
    models: 'Модели',
    addModel: 'Добавить',
    saving: 'Сохранение...',
    save: 'Сохранить',
    update: 'Обновить',
    cancel: 'Отмена',
    editProvider: 'Изменить {name}',
    addNewProvider: 'Добавить провайдера',
    promptTemplates: 'Шаблоны промптов',
    viewDefault: 'Показать оригинал',
    hideDefault: 'Скрыть оригинал',
    originalDefault: 'Исходный текст',
    delete: 'Удалить',
    selectPrompt: 'Выберите шаблон промпта для редактирования',
    customizedHint: 'Оранжевая точка = изменён',
    reset: 'Сбросить',
    resetToDefault: 'Сбросить к默认值',
    chars: '{count} симв.',
    charsDefault: '{count} симв. (по умолчанию)',
    promptLabels: {
      system_identity: 'Система — Идентичность',
      system_safety: 'Система — Правила безопасности',
      system_guidance: 'Система — Рекомендации по ответам',
      agent_general: 'Агент — Общий ассистент',
      agent_coder: 'Агент — Специалист по коду',
      agent_archivist: 'Агент — Архивариус памяти',
      agent_orchestrator: 'Агент — Оркестратор',
      agent_researcher: 'Агент — Исследователь',
      agent_critic: 'Агент — Критик',
      agent_planner: 'Агент — Планировщик',
      agent_summarizer: 'Агент — Суммаризатор',
      agent_help: 'Агент — Помощь',
      agent_tools_agent: 'Агент — Специалист по инструментам',
      agent_trigger_reactor: 'Агент — Реактор триггеров',
      summarizer_tool_output: 'Суммаризатор — Вывод инструментов',
      archivist_memory: 'Архивариус — Курация памяти',
      council_chair: 'Совет моделей — Синтез председателя',
      nudge_memory_gaps: 'Толчок — Пробелы в памяти',
      nudge_idle: 'Толчок — Напоминание о бездействии'
    }
  },

  dashboard: {
    title: 'Панель',
    threads: 'Треды',
    tools: 'Инструменты',
    agents: 'Агенты',
    pending: 'Ожидает',
    
    
    
    
    
    failedToLoad: 'Не удалось загрузить данные панели',
    retry: 'Повторить',
    refresh: 'Обновить',
    
    reflections: 'Подсознательные размышления',
    preferences: 'Preferences'
  },

  memory: {
    title: 'Поиск в памяти',
    searchPlaceholder: 'Ищите в своей памяти...',
    search: 'Поиск',
    searching: 'Поиск...',
    emptyHint: 'Поиск по всем сохранённым воспоминаниям',
    emptySub: 'Разговоры, факты, сущности и отношения',
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
    profile: 'Профиль'
  },

  todo: {
    title: 'Доска задач',
    addPlaceholder: 'Добавить задачу...',
    add: 'Добавить'
  },

  errors: {
    somethingWrong: 'Что-то пошло не так',
    
    
    
    tryAgain: 'Попробовать снова'
  },
  keyring: {
    title: 'Keyring',
    consentTitle: 'Storage Consent',
    consentBody: 'The OS keyring is not available. Store secrets locally?',
    consentAllow: 'Allow',
    consentDecline: 'Decline'
  },

  home: {
    welcome: 'Добро пожаловать в Mneme',
    tagline: 'Ваш ИИ-ассистент для управления разговорами, инструментами и памятью.',
    
    newChat: 'Новый чат',
    newChatDesc: 'Начать новый разговор с ИИ-агентом',
    searchMemory: 'Поиск в памяти',
    searchMemoryDesc: 'Поиск по разговорам, фактам и знаниям',
    manageTools: 'Управление инструментами',
    manageToolsDesc: 'Настройка инструментов, агентов и MCP-серверов',
    systemStatus: 'Состояние системы',
    providerConnected: 'Провайдер',
    connected: 'Подключен',
    notConnected: 'Не подключен',
    
    
    dbStatus: 'База данных'
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

export default ru;