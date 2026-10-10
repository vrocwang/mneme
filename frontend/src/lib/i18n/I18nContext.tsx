import React, { createContext, useContext, useState, useCallback, useMemo } from 'react';
import { StorageKeys, loadValue, saveValue } from '../storage';
import en from './en';
import type { EnTranslations } from './en';
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

// Every non-English locale is a deep-partial overlay of the English dictionary:
// only translated leaves are listed, and any unknown or misspelled key is a
// compile error.
export type DeepPartial<T> = {
  [K in keyof T]?: T[K] extends string ? T[K] : DeepPartial<T[K]>;
};

type Dict = Record<string, unknown>;

function isDict(value: unknown): value is Dict {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

// Deep-merge a locale overlay on top of the English base (objects recurse,
// leaves win) so a missing translation resolves to English, never a key path.
function deepMerge(base: Dict, overlay: unknown): Dict {
  const merged: Dict = { ...base };
  if (!isDict(overlay)) return merged;
  for (const [key, value] of Object.entries(overlay)) {
    const baseValue = merged[key];
    merged[key] = isDict(baseValue) ? deepMerge(baseValue, value) : value;
  }
  return merged;
}

const enDict: Dict = en as Dict;

// Applied once at module scope — never per t() call.
const withEnglishFallback = (overlay: DeepPartial<EnTranslations>): Dict => deepMerge(enDict, overlay);

const locales: Record<Locale, Dict> = {
  en: enDict,
  'zh-CN': withEnglishFallback(zhCN),
  de: withEnglishFallback(de),
  es: withEnglishFallback(es),
  fr: withEnglishFallback(fr),
  it: withEnglishFallback(it),
  pt: withEnglishFallback(pt),
  pl: withEnglishFallback(pl),
  ru: withEnglishFallback(ru),
  ko: withEnglishFallback(ko),
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
    const stored = loadValue(StorageKeys.locale) as Locale | null;
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
    saveValue(StorageKeys.locale, l);
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
