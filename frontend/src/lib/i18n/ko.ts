import type { EnTranslations } from './en';
import type { DeepPartial } from './I18nContext';

const ko: DeepPartial<EnTranslations> = {
  app: { title: 'Mneme' },

  nav: {
    home: '홈',
    chat: '채팅',
    memory: '메모리',
    tools: '도구',
    dashboard: '대시보드',
    notifications: '알림',
    settings: '설정'
  },

  sidebar: {
    threads: '스레드',
    newThread: '새 스레드',
    searchThreads: '스레드 검색...',
    noThreads: '아직 스레드 없음',
    noMatch: '일치하는 스레드 없음',
    msgs: '메시지',
    closeSidebar: '사이드바 닫기',
    deleteThread: '스레드 삭제',
    count: '{count}개 스레드',
    openSidebar: '사이드바 열기'
  },

  chat: {
    newConversation: '새 대화',
    selectThread: '스레드를 선택하거나 새로 만들어 시작하세요.',
    startConversation: '대화 시작',
    startHint: '메시지를 보내 시작하세요',
    placeholder: '메시지 보내기... (Enter로 전송, Shift+Enter로 줄바꿈)',
    
    
    
    you: '나',
    assistant: 'Mneme',
    toolResult: '도구 결과',
    tool: 'Tool',
    copy: 'Copy',
    copied: 'Copied!',
    thinkingShow: 'Show thinking',
    thinkingHide: 'Hide thinking',
    delete: '삭제',
    messages: '{count}개 메시지'
  },

  approval: {
    required: '승인 필요',
    wantsToExecute: '실행 요청',
    approveOnce: '한 번 승인',
    alwaysAllow: '항상 허용',
    deny: '거부',
    
    
    
    
    pending: '대기 중'
  },

  settings: {
    title: '설정',
    general: '일반',
    providers: '제공자',
    
    approvals: '승인',
    theme: '테마',
    config: '구성',
    prompts: '프롬프트',
    about: '정보',
    systemHealth: '시스템 상태',
    cronJobs: 'Cron 작업',
    
    
    
    
    noAllowlist: '영구적으로 허용된 도구가 없습니다.',
    
    version: '버전',
    toolsRegistered: '등록된 도구',
    agentsAvailable: '사용 가능한 에이전트',
    checkUpdates: '업데이트 확인',
    remove: '제거',
    appearance: '외관',
    soul: 'SOUL & Identity',
    channels: 'Channels',
    extensions: 'Extensions',
    
    
    
    
    darkLabel: '어두운 테마',
    darkDesc: '어두운 환경에서 눈이 편안합니다',
    lightLabel: '밝은 테마',
    lightDesc: '깨끗하고 밝은 외관',
    systemLabel: '시스템',
    systemDesc: 'OS 설정과 일치',
    agentLimits: '에이전트 제한',
    agentLimitsDesc: '에이전트 루프의 작동 제한.',
    maxToolRounds: '최대 도구 라운드',
    toolTimeout: '도구 타임아웃',
    security: '보안',
    securityDesc: '명령 분류 및 위험 설정.',
    blockHighRisk: '고위험 명령 차단',
    blockHighRiskDesc: 'shutdown, mkfs, dd, sudo와 같은 명령은 항상 차단됨',
    requireMediumApproval: '중간 위험 승인 필요',
    requireMediumApprovalDesc: '감독 모드에서 curl, apt, pip, git에 승인 필요',
    shellTool: '셸 도구',
    shellToolDesc: '셸 실행 제한.',
    maxOutputBytes: '최대 출력 바이트',
    memoryPipeline: '메모리 파이프라인',
    memoryPipelineDesc: '백그라운드 처리 매개변수.',
    workerCount: '워커 수',
    treeBucketSize: '트리 버킷 크기',
    archiveMsgLimit: '아카이브 메시지 제한',
    freshnessHalfLife: '신선도 반감기',
    retrievalWeights: '검색 가중치',
    retrievalWeightsDesc: '다중 전략 검색을 위한 신호 가중치. 합계가 1.0이어야 합니다.',
    sum: '합계',
    circuitBreaker: '회로 차단기',
    circuitBreakerDesc: '무한 루프 방지를 위해 에이전트를 중지하는 실패 임계값.',
    repeatFailures: '반복 실패 (동일 도구+인수)',
    noProgressFails: '진행 없는 실패 (연속)',
    costBudget: '비용 예산',
    costBudgetDesc: '추적을 위한 월간 API 비용 예산.',
    monthlyBudget: '월간 예산',
    dataDir: '데이터 디렉토리',
    dataDirDesc: '데이터베이스와 런타임 데이터가 저장되는 위치.',
    dataDirPlaceholder: '기본값: <workspace>/data',
    dataDirHint: '비워두면 workspace 디렉토리의 기본 위치를 사용합니다.',
    addProvider: '제공자 추가',
    defaultProvider: '기본 제공자',
    defaultProviderDesc: '채팅에 사용할 제공자 선택',
    modelRouting: '모델 라우팅',
    modelRoutingCoding: '코딩',
    modelRoutingReasoning: '추론',
    modelRoutingSummary: '요약',
    modelRoutingVision: '비전',
    useDefault: '기본값',
    
    
    noProviders: '구성된 제공자가 없습니다.',
    noProvidersHint: '시작하려면 제공자를 추가하거나 환경 변수로 API 키를 설정하세요.',
    edit: '편집',
    name: '이름',
    type: '유형',
    apiKey: 'API 키',
    baseUrl: '기본 URL',
    models: '모델',
    addModel: '추가',
    saving: '저장 중...',
    save: '저장',
    update: '업데이트',
    cancel: '취소',
    editProvider: '{name} 편집',
    addNewProvider: '제공자 추가',
    promptTemplates: '프롬프트 템플릿',
    viewDefault: '원본 보기',
    hideDefault: '원본 숨기기',
    originalDefault: '기본 원본',
    delete: '삭제',
    selectPrompt: '편집할 프롬프트 템플릿 선택',
    customizedHint: '주황색 점 = 사용자 정의됨',
    reset: '초기화',
    resetToDefault: '기본값으로 초기화',
    chars: '{count}자',
    charsDefault: '{count}자 (기본값)',
    promptLabels: {
      system_identity: '시스템 — 정체성',
      system_safety: '시스템 — 안전 규칙',
      system_guidance: '시스템 — 응답 지침',
      agent_general: '에이전트 — 일반 어시스턴트',
      agent_coder: '에이전트 — 코딩 전문가',
      agent_archivist: '에이전트 — 메모리 기록관',
      agent_orchestrator: '에이전트 — 오케스트레이터',
      agent_researcher: '에이전트 — 연구원',
      agent_critic: '에이전트 — 비평가',
      agent_planner: '에이전트 — 계획자',
      agent_summarizer: '에이전트 — 요약기',
      agent_help: '에이전트 — 도움말',
      agent_tools_agent: '에이전트 — 도구 전문가',
      agent_trigger_reactor: '에이전트 — 트리거 반응기',
      summarizer_tool_output: '요약기 — 도구 출력',
      archivist_memory: '기록관 — 메모리 큐레이션',
      council_chair: '모델 협의회 — 의장 종합',
      nudge_memory_gaps: '넛지 — 메모리 공백',
      nudge_idle: '넛지 — 유휴 알림'
    }
  },

  dashboard: {
    title: '대시보드',
    threads: '스레드',
    tools: '도구',
    agents: '에이전트',
    pending: '대기 중',
    
    
    
    
    
    failedToLoad: '대시보드 데이터 로드 실패',
    retry: '재시도',
    refresh: '새로고침',
    
    reflections: '잠재의식 성찰',
    preferences: 'Preferences'
  },

  memory: {
    title: '메모리 검색',
    searchPlaceholder: '메모리 검색...',
    search: '검색',
    searching: '검색 중...',
    emptyHint: '저장된 모든 메모리 검색',
    emptySub: '대화, 사실, 엔티티 및 관계',
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
    profile: '프로필'
  },

  todo: {
    title: '작업 보드',
    addPlaceholder: '작업 추가...',
    add: '추가'
  },

  errors: {
    somethingWrong: '문제가 발생했습니다',
    
    
    
    tryAgain: '다시 시도'
  },
  keyring: {
    title: 'Keyring',
    consentTitle: 'Storage Consent',
    consentBody: 'The OS keyring is not available. Store secrets locally?',
    consentAllow: 'Allow',
    consentDecline: 'Decline'
  },

  home: {
    welcome: 'Welcome to Mneme',
    tagline: 'Your AI assistant for managing conversations, tools, and memory.',
    
    newChat: 'New Chat',
    newChatDesc: 'Start a new conversation with an AI agent',
    searchMemory: 'Search Memory',
    searchMemoryDesc: 'Search across conversations, facts, and knowledge',
    manageTools: 'Manage Tools',
    manageToolsDesc: 'Configure tools, agents, and MCP servers',
    systemStatus: 'System Status',
    providerConnected: 'Provider',
    connected: 'Connected',
    notConnected: 'Not Connected',
    
    
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

export default ko;