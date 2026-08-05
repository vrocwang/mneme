import React, { createContext, useContext, useState, useCallback, useMemo } from 'react';
import en from './en';
import zhCN from './zh-CN';
import de from './de';
import es from './es';
import fr from './fr';
import it from './it';
import pt from './pt';
import pl from './pl';
import ru from './ru';
import ko from './ko';

export type Locale = 'en' | 'zh-CN' | 'de' | 'es' | 'fr' | 'it' | 'pt' | 'pl' | 'ru' | 'ko';

// Use a looser type here — typeof en creates an exact brand that
// doesn't match structurally identical objects from other modules.
const locales: Record<string, Record<string, unknown>> = {
  en: en as unknown as Record<string, unknown>,
  'zh-CN': zhCN as unknown as Record<string, unknown>,
  de: de as unknown as Record<string, unknown>,
  es: es as unknown as Record<string, unknown>,
  fr: fr as unknown as Record<string, unknown>,
  it: it as unknown as Record<string, unknown>,
  pt: pt as unknown as Record<string, unknown>,
  pl: pl as unknown as Record<string, unknown>,
  ru: ru as unknown as Record<string, unknown>,
  ko: ko as unknown as Record<string, unknown>,
};

const localeLabels: Record<Locale, string> = {
  en: 'English',
  'zh-CN': '简体中文',
  de: 'Deutsch',
  es: 'Español',
  fr: 'Français',
  it: 'Italiano',
  pt: 'Português',
  pl: 'Polski',
  ru: 'Русский',
  ko: '한국어',
};

interface I18nContextValue {
  locale: Locale;
  setLocale: (l: Locale) => void;
  t: (path: string, fallbackOrVars?: string | Record<string, string | number>) => string;
  localeLabels: Record<Locale, string>;
}

const I18nContext = createContext<I18nContextValue | null>(null);

// Map browser language codes to our locales.
const browserLocaleMap: Record<string, Locale> = {
  zh: 'zh-CN', 'zh-CN': 'zh-CN', 'zh-TW': 'zh-CN', 'zh-HK': 'zh-CN',
  de: 'de', 'de-DE': 'de', 'de-AT': 'de', 'de-CH': 'de',
  es: 'es', 'es-ES': 'es', 'es-MX': 'es', 'es-AR': 'es', 'es-CO': 'es', 'es-CL': 'es', 'es-PE': 'es',
  fr: 'fr', 'fr-FR': 'fr', 'fr-CA': 'fr', 'fr-BE': 'fr', 'fr-CH': 'fr',
  it: 'it', 'it-IT': 'it', 'it-CH': 'it',
  pt: 'pt', 'pt-PT': 'pt', 'pt-BR': 'pt',
  pl: 'pl', 'pl-PL': 'pl',
  ru: 'ru', 'ru-RU': 'ru',
  ko: 'ko', 'ko-KR': 'ko',
};

function detectLocale(): Locale {
  try {
    const stored = localStorage.getItem('mneme-locale') as Locale | null;
    if (stored && locales[stored]) return stored;
    const nav = navigator.language || '';
    if (browserLocaleMap[nav]) return browserLocaleMap[nav];
    const base = nav.split('-')[0];
    if (browserLocaleMap[base]) return browserLocaleMap[base];
  } catch {}
  return 'en';
}

function getNested(obj: Record<string, unknown>, path: string): string {
  const parts = path.split('.');
  let current: unknown = obj;
  for (const part of parts) {
    if (current == null || typeof current !== 'object') return '';
    current = (current as Record<string, unknown>)[part];
  }
  return typeof current === 'string' ? current as string : '';
}

export function I18nProvider({ children }: { children: React.ReactNode }) {
  const [locale, setLocale] = useState<Locale>(detectLocale);

  const handleSetLocale = useCallback((l: Locale) => {
    setLocale(l);
    try { localStorage.setItem('mneme-locale', l); } catch {}
  }, []);

  const t = useCallback((path: string, fallbackOrVars?: string | Record<string, string | number>): string => {
    const translations = locales[locale] || {};
    let value: string = getNested(translations, path);
    if (!value && typeof fallbackOrVars === 'string') value = fallbackOrVars;
    if (!value) return path;
    if (fallbackOrVars && typeof fallbackOrVars === 'object') {
      for (const [k, v] of Object.entries(fallbackOrVars)) {
        value = value.replaceAll(`{${k}}`, String(v));
      }
    }
    return value;
  }, [locale]);

  const value = useMemo(() => ({ locale, setLocale: handleSetLocale, t, localeLabels }), [locale, handleSetLocale, t]);

  return (
    <I18nContext.Provider value={value}>
      {children}
    </I18nContext.Provider>
  );
}

export function useT() {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error('useT must be used within I18nProvider');
  return { t: ctx.t, locale: ctx.locale, setLocale: ctx.setLocale, localeLabels: ctx.localeLabels };
}
