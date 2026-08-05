import { useEffect, useState, useCallback } from 'react';
import { load, save } from '../lib/persist';

type Theme = 'dark' | 'light' | 'system';

const STORAGE_KEY = 'theme';

export function getTheme(): Theme {
  return (load(STORAGE_KEY) as Theme) || 'dark';
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
  save(STORAGE_KEY, theme);
  applyTheme(theme);
}

export function ThemeSwitcher() {
  const [current, setCurrent] = useState<Theme>(getTheme);

  const handleChange = useCallback(() => {
    const next: Theme = current === 'dark' ? 'light' : current === 'light' ? 'system' : 'dark';
    setCurrent(next);
    setTheme(next);
  }, [current]);

  // Listen for system theme changes when in system mode.
  useEffect(() => {
    applyTheme(current);
    if (current !== 'system') return;
    const mq = window.matchMedia('(prefers-color-scheme: dark)');
    const handler = () => applyTheme('system');
    mq.addEventListener('change', handler);
    return () => mq.removeEventListener('change', handler);
  }, [current]);

  const icon = current === 'dark' ? '🌙' : current === 'light' ? '☀️' : '🖥️';
  const nextLabel = current === 'dark' ? 'light' : current === 'light' ? 'system' : 'dark';

  return (
    <button
      onClick={handleChange}
      className="text-xs text-white/40 hover:text-white/70 transition-colors"
      title={`Theme: ${current} (click for ${nextLabel})`}
    >
      {icon}
    </button>
  );
}
