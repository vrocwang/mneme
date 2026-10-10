import { useCallback, useEffect, useSyncExternalStore } from 'react';
import { StorageKeys, loadValue, saveValue } from '../lib/storage';
import { useT } from '../lib/i18n/I18nContext';

export type Theme = 'dark' | 'light' | 'system';

// i18n key for each theme name, shared by the switcher tooltip and Settings.
const THEME_LABEL_KEYS: Record<Theme, string> = {
  dark: 'settings.darkLabel',
  light: 'settings.lightLabel',
  system: 'settings.systemLabel',
};

// Module-level store: the applied theme has one owner, so the switcher and the
// Settings appearance tab can never disagree about it.
let currentTheme: Theme = (loadValue(StorageKeys.theme) as Theme) || 'dark';
const listeners = new Set<() => void>();

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

export function getTheme(): Theme {
  return currentTheme;
}

function resolveSystemTheme(): 'dark' | 'light' {
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

export function applyTheme(theme: Theme) {
  const root = document.documentElement;
  const effective = theme === 'system' ? resolveSystemTheme() : theme;
  // Dark is the default (no class). Light gets the explicit class.
  if (effective === 'light') {
    root.classList.add('light');
  } else {
    root.classList.remove('light');
  }
}

export function setTheme(theme: Theme) {
  currentTheme = theme;
  saveValue(StorageKeys.theme, theme);
  applyTheme(theme);
  listeners.forEach(listener => listener());
}

// useTheme reads the shared theme and applies it, tracking OS changes while in
// system mode. Every consumer renders the same value.
export function useTheme(): Theme {
  const theme = useSyncExternalStore(subscribe, getTheme, getTheme);

  useEffect(() => {
    applyTheme(theme);
    if (theme !== 'system') return;
    const mq = window.matchMedia('(prefers-color-scheme: dark)');
    const handler = () => applyTheme('system');
    mq.addEventListener('change', handler);
    return () => mq.removeEventListener('change', handler);
  }, [theme]);

  return theme;
}

// Click order of the switcher: dark → light → system → dark.
const THEME_CYCLE: Theme[] = ['dark', 'light', 'system'];

function nextTheme(current: Theme): Theme {
  return THEME_CYCLE[(THEME_CYCLE.indexOf(current) + 1) % THEME_CYCLE.length];
}

export function ThemeSwitcher() {
  const { t } = useT();
  const current = useTheme();
  const next = nextTheme(current);

  const handleChange = useCallback(() => {
    setTheme(nextTheme(current));
  }, [current]);

  const icon = current === 'dark' ? '🌙' : current === 'light' ? '☀️' : '🖥️';

  return (
    <button
      onClick={handleChange}
      className="text-xs text-white/40 hover:text-white/70 transition-colors"
      title={t('common.themeTooltip', {
        current: t(THEME_LABEL_KEYS[current]),
        next: t(THEME_LABEL_KEYS[next]),
      })}
    >
      {icon}
    </button>
  );
}
