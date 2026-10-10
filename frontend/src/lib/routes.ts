// Route table. The router, the sidebar, and in-app links all read from here so
// a path never has to be spelled as a literal in more than one place.

export const ROUTES = {
  home: '/',
  chat: '/chat',
  memory: '/memory',
  capabilities: '/capabilities',
  dashboard: '/dashboard',
  notifications: '/notifications',
  monitor: '/monitor',
  settings: '/settings',
  reset: '/reset',
} as const;

export type RouteId =
  | 'home'
  | 'chat'
  | 'memory'
  | 'capabilities'
  | 'dashboard'
  | 'notifications'
  | 'monitor'
  | 'settings';

// Sidebar order; `labelKey` resolves through i18n under `nav.*`.
export const SIDEBAR_ITEMS: readonly { id: RouteId; labelKey: string }[] = [
  { id: 'home', labelKey: 'nav.home' },
  { id: 'chat', labelKey: 'nav.chat' },
  { id: 'memory', labelKey: 'nav.memory' },
  { id: 'capabilities', labelKey: 'nav.capabilities' },
  { id: 'dashboard', labelKey: 'nav.dashboard' },
  { id: 'notifications', labelKey: 'nav.notifications' },
  { id: 'monitor', labelKey: 'nav.monitor' },
  { id: 'settings', labelKey: 'nav.settings' },
];

export function chatPath(threadId: string): string {
  return `${ROUTES.chat}/${threadId}`;
}

/** Path without the leading slash, for react-router's nested <Route path>. */
export function routeSegment(id: RouteId): string {
  return ROUTES[id].replace(/^\//, '');
}

/** CSS selector for a sidebar entry; the walkthrough highlights these. */
export function navSelector(id: RouteId): string {
  return `[data-nav="${id}"]`;
}

export function isRouteActive(id: RouteId, pathname: string): boolean {
  return pathname === ROUTES[id];
}
