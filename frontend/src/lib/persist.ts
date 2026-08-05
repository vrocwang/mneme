// Lightweight client-side state persistence using localStorage.
// Persists sidebar state, active view, and locale choice.

const PREFIX = 'mneme:';

export function load(key: string, fallback: string | null = null): string | null {
  try {
    const val = localStorage.getItem(PREFIX + key);
    return val !== null ? val : fallback;
  } catch {
    return fallback;
  }
}

export function save(key: string, value: string): void {
  try {
    localStorage.setItem(PREFIX + key, value);
  } catch {
    // Storage full or unavailable — silently ignore.
  }
}

