// Single registry for everything the app persists in browser storage.
//
// The values are load-bearing. `sidebar` and `theme` go through lib/persist,
// which writes them as `mneme:sidebar` / `mneme:theme`; `onboarding_done`,
// `walkthrough_done` and `mneme-locale` predate that scheme and live under
// their bare names. Renaming any value silently discards state that an
// existing user already has, so treat them as frozen.

import { load, loadRaw, remove, removeRaw, save, saveRaw } from './persist';

// PersistRoot is the redux-persist namespace: it writes `persist:mneme`, a third
// scheme that predates the two above. It lives here so all persisted state is
// discoverable from one place; changing it discards the persisted thread slice.
export const PersistRoot = 'mneme';

export const StorageKeys = {
  sidebarOpen: 'sidebar',
  theme: 'theme',
  onboardingDone: 'onboarding_done',
  walkthroughDone: 'walkthrough_done',
  locale: 'mneme-locale',
} as const;

export type StorageKey = (typeof StorageKeys)[keyof typeof StorageKeys];

// Keys written through lib/persist, i.e. carrying the `mneme:` prefix.
const PREFIXED: ReadonlySet<string> = new Set<string>([StorageKeys.sidebarOpen, StorageKeys.theme]);

export function loadValue(key: StorageKey, fallback: string | null = null): string | null {
  return PREFIXED.has(key) ? load(key, fallback) : loadRaw(key, fallback);
}

export function saveValue(key: StorageKey, value: string): void {
  if (PREFIXED.has(key)) save(key, value);
  else saveRaw(key, value);
}

export function removeValue(key: StorageKey): void {
  if (PREFIXED.has(key)) remove(key);
  else removeRaw(key);
}

/** Reads a boolean flag; anything other than `true` counts as false. */
export function loadFlag(key: StorageKey, fallback = false): boolean {
  const raw = loadValue(key);
  return raw === null ? fallback : raw === 'true';
}

export function saveFlag(key: StorageKey, value: boolean): void {
  saveValue(key, String(value));
}
