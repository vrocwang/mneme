import React, { createContext, useContext, useReducer, useCallback, useRef, useEffect, useMemo } from 'react';
import { StorageKeys, loadValue, saveValue } from '../lib/storage';
import { TOAST_TIMEOUT_MS } from '../lib/timing';

// ── State ─────────────────────────────────────────────────────────────────
// AppContext retains only transient UI state (sidebar, toasts).
// All domain data — threads, messages, streaming, approvals — lives in Redux.

interface AppState {
  sidebarOpen: boolean;
  toasts: Toast[];
}

export interface Toast {
  id: string;
  kind: 'info' | 'success' | 'warning' | 'error';
  title: string;
  body: string;
}

type Action =
  | { type: 'TOGGLE_SIDEBAR' }
  | { type: 'ADD_TOAST'; toast: Toast }
  | { type: 'REMOVE_TOAST'; id: string };

function reducer(state: AppState, action: Action): AppState {
  switch (action.type) {
    case 'TOGGLE_SIDEBAR':
      // Persistence is handled by the effect in AppProvider; reducers must stay
      // pure (React may invoke them twice under StrictMode/concurrent renders).
      return { ...state, sidebarOpen: !state.sidebarOpen };
    case 'ADD_TOAST':
      return { ...state, toasts: [...state.toasts, action.toast] };
    case 'REMOVE_TOAST':
      return { ...state, toasts: state.toasts.filter(t => t.id !== action.id) };
    default:
      return state;
  }
}

const initialState: AppState = {
  sidebarOpen: loadValue(StorageKeys.sidebarOpen) !== 'false',
  toasts: [],
};

// ── Context ───────────────────────────────────────────────────────────────

interface AppContextValue {
  state: AppState;
  dispatch: React.Dispatch<Action>;
  actions: AppActions;
}

export interface AppActions {
  toggleSidebar: () => void;
  addToast: (kind: Toast['kind'], title: string, body: string) => void;
}

const AppContext = createContext<AppContextValue | null>(null);

export function useApp() {
  const ctx = useContext(AppContext);
  if (!ctx) throw new Error('useApp must be used within AppProvider');
  return ctx;
}

// ── Provider ──────────────────────────────────────────────────────────────

export function AppProvider({ children }: { children: React.ReactNode }) {
  const [state, dispatch] = useReducer(reducer, initialState);
  const toastIdRef = useRef(0);

  const toggleSidebar = useCallback(() => {
    dispatch({ type: 'TOGGLE_SIDEBAR' });
  }, []);

  const addToast = useCallback((kind: Toast['kind'], title: string, body: string) => {
    const id = `toast-${++toastIdRef.current}`;
    dispatch({ type: 'ADD_TOAST', toast: { id, kind, title, body } });
    setTimeout(() => dispatch({ type: 'REMOVE_TOAST', id }), TOAST_TIMEOUT_MS);
  }, []);

  useEffect(() => {
    saveValue(StorageKeys.sidebarOpen, String(state.sidebarOpen));
  }, [state.sidebarOpen]);

  // Memoize so `actions` keeps a stable identity; consumers list it in their
  // useCallback deps, and a fresh object each render would invalidate them.
  const value = useMemo(
    () => ({ state, dispatch, actions: { toggleSidebar, addToast } }),
    [state, dispatch, toggleSidebar, addToast],
  );

  return (
    <AppContext.Provider value={value}>
      {children}
    </AppContext.Provider>
  );
}
