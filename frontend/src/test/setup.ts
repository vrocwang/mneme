// Vitest setup — configures the test environment for React component testing.
// Run with: npx vitest
//
// Add vitest to devDependencies:
//   npm install --save-dev vitest @testing-library/react @testing-library/jest-dom jsdom

import '@testing-library/jest-dom';

// Mock the Wails Go backend for tests.
if (typeof window !== 'undefined') {
  (window as any).go = {
    main: {
      App: {
        Health: async () => ({ ok: true, tools: 5, agents: 3 }),
        ListAgents: async () => [{ id: 'general', name: 'General' }],
        ListPendingApprovals: async () => [],
        KeyringStatus: async () => ({ available: true, activeMode: 'os_keyring', backendName: 'os' }),
        GetToolDiagnostics: async () => ({ totalTools: 10, enabledTools: 10 }),
        ListTunnels: async () => [],
        GetCostOverview: async () => ({ daily: 0, monthly: 0 }),
      },
    },
  };
}

// Suppress console errors in tests.
const originalError = console.error;
beforeAll(() => {
  console.error = (...args: any[]) => {
    if (typeof args[0] === 'string' && args[0].includes('React')) return;
    originalError.call(console, ...args);
  };
});

afterAll(() => {
  console.error = originalError;
});
