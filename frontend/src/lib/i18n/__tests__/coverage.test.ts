// Verifies that all locale files have the same keys as en.ts.
// Run with: npx vitest run

import { describe, it, expect } from 'vitest';
import en from '../en';

// Lazy-load other locales to avoid bundling in production
const locales: Record<string, () => Promise<any>> = {
  de: () => import('../de'),
  es: () => import('../es'),
  fr: () => import('../fr'),
  it: () => import('../it'),
  ko: () => import('../ko'),
  pl: () => import('../pl'),
  pt: () => import('../pt'),
  ru: () => import('../ru'),
  'zh-CN': () => import('../zh-CN'),
};

function getAllKeys(obj: any, prefix = ''): string[] {
  const keys: string[] = [];
  for (const key of Object.keys(obj)) {
    const fullKey = prefix ? `${prefix}.${key}` : key;
    if (typeof obj[key] === 'object' && obj[key] !== null && !Array.isArray(obj[key])) {
      keys.push(...getAllKeys(obj[key], fullKey));
    } else {
      keys.push(fullKey);
    }
  }
  return keys.sort();
}

function getValue(obj: any, path: string): any {
  return path.split('.').reduce((o, k) => (o != null ? o[k] : undefined), obj);
}

describe('i18n coverage', () => {
  const enKeys = getAllKeys(en);

  it('en.ts should have translation keys', () => {
    expect(enKeys.length).toBeGreaterThan(100);
  });

  // Test that all required sections exist in en.ts
  const requiredSections = ['app', 'nav', 'sidebar', 'chat', 'settings', 'memory', 'errors',
    'keyring', 'extensions', 'webhooks', 'autonomy', 'diagnostics'];

  requiredSections.forEach(section => {
    it(`en.ts should have '${section}' section`, () => {
      expect(en).toHaveProperty(section);
    });
  });

  // Test each locale for key parity with en.ts
  Object.entries(locales).forEach(([locale, loader]) => {
    it(`${locale}.ts should have all keys from en.ts`, async () => {
      const mod = await loader();
      const localeData = mod.default || mod;
      const missingKeys: string[] = [];

      for (const key of enKeys) {
        if (getValue(localeData, key) === undefined) {
          missingKeys.push(key);
        }
      }

      if (missingKeys.length > 0) {
        // List first 10 missing keys for diagnostics
        expect(missingKeys.slice(0, 10).join(', ')).toBe('no missing keys');
      }
      expect(missingKeys).toHaveLength(0);
    });
  });
});
