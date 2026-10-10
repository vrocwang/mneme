import { useState, useEffect, useCallback } from 'react';
import { useT } from '../lib/i18n/I18nContext';
import { CONNECTIVITY_POLL_MS } from '../lib/timing';
import * as api from '../services/wails';

type Status = 'online' | 'offline' | 'checking' | 'degraded';

export function ConnectivityIndicator() {
  const { t } = useT();
  const [coreStatus, setCoreStatus] = useState<Status>('checking');
  const [backendStatus, setBackendStatus] = useState<Status>('checking');
  const [internetStatus, setInternetStatus] = useState<Status>('checking');

  const check = useCallback(async () => {
    setInternetStatus(navigator.onLine ? 'online' : 'offline');
    try {
      const report = await api.getDoctorReport();
      // report.ok is true when all health checks pass, false if any are degraded/failed
      if (!report.ok) {
        setCoreStatus('degraded');
      } else {
        setCoreStatus('online');
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
    const interval = setInterval(check, CONNECTIVITY_POLL_MS);
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
      <span className="flex items-center gap-1">
        <span className={`inline-block w-1.5 h-1.5 rounded-full ${dot(internetStatus)}`} />
        <span className="sr-only">{t('common.internet')}</span>
        <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-label={t('common.internet')}>
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3.055 11H5a2 2 0 012 2v1a2 2 0 002 2 2 2 0 012 2v2.945M8 3.935V5.5A2.5 2.5 0 0010.5 8h.5a2 2 0 012 2 2 2 0 104 0 2 2 0 012-2h1.064M15 20.488V18a2 2 0 012-2h3.064M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
      </span>
      <span className="flex items-center gap-1">
        <span className={`inline-block w-1.5 h-1.5 rounded-full ${dot(coreStatus)}`} />
        {t('common.core')}
      </span>
      <span className="flex items-center gap-1">
        <span className={`inline-block w-1.5 h-1.5 rounded-full ${dot(backendStatus)}`} />
        {t('common.api')}
      </span>
    </div>
  );
}
