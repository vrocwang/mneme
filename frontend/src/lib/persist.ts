// Lightweight client-side state persistence using localStorage.
// `load`/`save`/`remove` operate on the `mneme:`-prefixed keys; the `*Raw`
// variants take the exact storage key for entries that predate the prefix.
// Feature code should go through lib/storage instead of calling these directly.

const PREFIX = 'mneme:';

export function loadRaw(key: string, fallback: string | null = null): string | null {
  try {
    const val = localStorage.getItem(key);
    return val !== null ? val : fallback;
  } catch {
    return fallback;
  }
}

export function saveRaw(key: string, value: string): void {
  try {
    localStorage.setItem(key, value);
  } catch {
    // Storage full or unavailable — silently ignore.
  }
}

export function removeRaw(key: string): void {
  try {
    localStorage.removeItem(key);
  } catch {
    // Storage unavailable — silently ignore.
  }
}

export function load(key: string, fallback: string | null = null): string | null {
  return loadRaw(PREFIX + key, fallback);
}

export function save(key: string, value: string): void {
  saveRaw(PREFIX + key, value);
}

export function remove(key: string): void {
  removeRaw(PREFIX + key);
}
