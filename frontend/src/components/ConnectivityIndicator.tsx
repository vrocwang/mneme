import { useState, useEffect, useCallback } from 'react';
import * as api from '../services/wails';

type Status = 'online' | 'offline' | 'checking' | 'degraded';

export function ConnectivityIndicator() {
  const [coreStatus, setCoreStatus] = useState<Status>('checking');
  const [backendStatus, setBackendStatus] = useState<Status>('checking');
  const [internetStatus, setInternetStatus] = useState<Status>('checking');
  const [startupErrors, setStartupErrors] = useState<string[]>([]);
  const [showErrors, setShowErrors] = useState(false);

  const check = useCallback(async () => {
    setInternetStatus(navigator.onLine ? 'online' : 'offline');
    try {
      const report = await api.getDoctorReport();
      // report.ok is true when all health checks pass, false if any are degraded/failed
      if (!report.ok) {
        setCoreStatus('degraded');
      } else {
        setCoreStatus('online');
        setStartupErrors([]);
      }
      // API dot reflects the LLM provider health check
      const providerCheck = (report.checks as Array<{name: string; status: string; message: string}>)?.find(
        (c: {name: string}) => c.name === 'provider'
      );
      if (providerCheck && providerCheck.status === 'ok') {
        setBackendStatus('online');
      } else if (providerCheck) {
        setBackendStatus('degraded');
      } else {
        // If provider check is missing, rely on overall status
        setBackendStatus(report.ok ? 'online' : 'degraded');
      }
    } catch {
      setCoreStatus('offline');
      setBackendStatus('offline');
    }
  }, []);

  useEffect(() => {
    check();
    const handleOnline = () => setInternetStatus('online');
    const handleOffline = () => setInternetStatus('offline');
    window.addEventListener('online', handleOnline);
    window.addEventListener('offline', handleOffline);
    const interval = setInterval(check, 30000);
    return () => {
      window.removeEventListener('online', handleOnline);
      window.removeEventListener('offline', handleOffline);
      clearInterval(interval);
    };
  }, [check]);

  const dot = (s: Status) =>
    s === 'online' ? 'bg-sage-400'
    : s === 'degraded' ? 'bg-amber-400 animate-pulse-soft'
    : s === 'offline' ? 'bg-coral-400'
    : 'bg-amber-400 animate-pulse-soft';

  return (
    <div className="flex items-center gap-3 text-xs text-white/30">
      {startupErrors.length > 0 && (
        <div className="relative">
          <button
            onClick={() => setShowErrors(!showErrors)}
            className="flex items-center gap-1 text-amber-400 hover:text-amber-300 transition-colors"
            title="Startup errors detected"
          >
            <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-2.694-.833-3.464 0L3.34 16.5c-.77.833.192 2.5 1.732 2.5z" />
            </svg>
            <span>{startupErrors.length}</span>
          </button>
          {showErrors && (
            <div className="absolute bottom-full left-0 mb-2 w-80 bg-surface-elevated border border-surface-border rounded-lg shadow-xl p-3 z-50">
              <p className="text-xs font-medium text-amber-400 mb-1.5">Startup Errors</p>
              <ul className="space-y-1 max-h-48 overflow-y-auto">
                {startupErrors.map((err, i) => (
                  <li key={i} className="text-xs text-white/50 font-mono break-all">{err}</li>
                ))}
              </ul>
              <button
                onClick={(e) => { e.stopPropagation(); setShowErrors(false); }}
                className="mt-2 text-xs text-white/40 hover:text-white/60"
              >
                Dismiss
              </button>
            </div>
          )}
        </div>
      )}
      <span className="flex items-center gap-1">
        <span className={`inline-block w-1.5 h-1.5 rounded-full ${dot(internetStatus)}`} />
        <span className="sr-only">Internet</span>
        <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-label="Internet">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3.055 11H5a2 2 0 012 2v1a2 2 0 002 2 2 2 0 012 2v2.945M8 3.935V5.5A2.5 2.5 0 0010.5 8h.5a2 2 0 012 2 2 2 0 104 0 2 2 0 012-2h1.064M15 20.488V18a2 2 0 012-2h3.064M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
      </span>
      <span className="flex items-center gap-1">
        <span className={`inline-block w-1.5 h-1.5 rounded-full ${dot(coreStatus)}`} />
        Core
      </span>
      <span className="flex items-center gap-1">
        <span className={`inline-block w-1.5 h-1.5 rounded-full ${dot(backendStatus)}`} />
        API
      </span>
    </div>
  );
}
