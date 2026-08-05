import React, { createContext, useContext, useReducer, useCallback, useRef, useEffect } from 'react';
import { load, save } from '../lib/persist';

// ── State ─────────────────────────────────────────────────────────────────
// AppContext retains only transient UI state (view, sidebar, toasts).
// All domain data — threads, messages, streaming, approvals — lives in Redux.

interface AppState {
  view: 'home' | 'chat' | 'settings' | 'dashboard' | 'memory' | 'capabilities' | 'notifications';
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
  | { type: 'SET_VIEW'; view: AppState['view'] }
  | { type: 'TOGGLE_SIDEBAR' }
  | { type: 'ADD_TOAST'; toast: Toast }
  | { type: 'REMOVE_TOAST'; id: string };

function reducer(state: AppState, action: Action): AppState {
  switch (action.type) {
    case 'SET_VIEW':
      save('view', action.view);
      return { ...state, view: action.view };
    case 'TOGGLE_SIDEBAR':
      save('sidebar', String(!state.sidebarOpen));
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
  view: (load('view') as AppState['view']) || 'chat',
  sidebarOpen: load('sidebar') !== 'false',
  toasts: [],
};

// ── Context ───────────────────────────────────────────────────────────────

interface AppContextValue {
  state: AppState;
  dispatch: React.Dispatch<Action>;
  actions: AppActions;
}

export interface AppActions {
  setView: (view: AppState['view']) => void;
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

  const setView = useCallback((view: AppState['view']) => {
    dispatch({ type: 'SET_VIEW', view });
  }, []);

  const toggleSidebar = useCallback(() => {
    dispatch({ type: 'TOGGLE_SIDEBAR' });
  }, []);

  const addToast = useCallback((kind: Toast['kind'], title: string, body: string) => {
    const id = `toast-${++toastIdRef.current}`;
    dispatch({ type: 'ADD_TOAST', toast: { id, kind, title, body } });
    setTimeout(() => dispatch({ type: 'REMOVE_TOAST', id }), 5000);
  }, []);

  useEffect(() => {
    save('view', state.view);
    save('sidebar', String(state.sidebarOpen));
  }, [state.view, state.sidebarOpen]);

  return (
    <AppContext.Provider value={{ state, dispatch, actions: { setView, toggleSidebar, addToast } }}>
      {children}
    </AppContext.Provider>
  );
}
