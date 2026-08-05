import { useApp } from '../../state/AppContext';

const ICONS: Record<string, string> = {
  info: 'M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z',
  success: 'M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z',
  warning: 'M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-2.694-.833-3.464 0L3.34 16.5c-.77.833.192 2.5 1.732 2.5z',
  error: 'M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z',
};

const COLORS: Record<string, string> = {
  info: 'border-ocean-500/30 bg-ocean-500/10',
  success: 'border-sage-500/30 bg-sage-500/10',
  warning: 'border-amber-500/30 bg-amber-500/10',
  error: 'border-coral-500/30 bg-coral-500/10',
};

export function NotificationToast() {
  const { state, dispatch } = useApp();

  return (
    <div className="fixed bottom-4 right-4 z-50 space-y-2 max-w-sm">
      {state.toasts.map(toast => (
        <div
          key={toast.id}
          className={`animate-slide-up flex items-start gap-3 p-4 rounded-xl border backdrop-blur-xl shadow-overlay ${COLORS[toast.kind]}`}
        >
          <svg className="w-5 h-5 shrink-0 mt-0.5 text-white/70" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d={ICONS[toast.kind] || ICONS.info} />
          </svg>
          <div className="flex-1 min-w-0">
            <div className="text-sm font-medium text-white/90">{toast.title}</div>
            {toast.body && <div className="text-xs text-white/50 mt-0.5">{toast.body}</div>}
          </div>
          <button
            className="shrink-0 text-white/30 hover:text-white/60 transition-colors"
            onClick={() => dispatch({ type: 'REMOVE_TOAST', id: toast.id })}
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>
      ))}
    </div>
  );
}
