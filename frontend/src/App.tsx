import { useState, useCallback, useEffect } from 'react';
import { Provider } from 'react-redux';
import { PersistGate } from 'redux-persist/integration/react';
import { store, persistor } from './store';
import { I18nProvider } from './lib/i18n/I18nContext';
import { AppProvider } from './state/AppContext';
import AppRouter from './AppRouter';
import { OnboardingWizard } from './components/onboarding/OnboardingWizard';
import { KeyringConsentOverlay } from './components/keyring/KeyringConsentOverlay';
import { Walkthrough } from './components/walkthrough/Walkthrough';
import * as api from './services/wails';

export default function App() {
  const [showOnboarding, setShowOnboarding] = useState<boolean | null>(null);
  const handleComplete = useCallback(() => setShowOnboarding(false), []);

  // Determine whether onboarding should show:
  // 1. If user explicitly completed it before (localStorage), skip — unless
  //    it's a fresh install with no threads and no providers.
  // 2. If it's a truly fresh install (no data at all), always show.
  useEffect(() => {
    let cancelled = false;
    const check = async () => {
      const wasDone = localStorage.getItem('onboarding_done') === 'true';
      if (!wasDone) { if (!cancelled) setShowOnboarding(true); return; }

      // Even if previously completed, re-show if the workspace is empty
      // (fresh reinstall or data was wiped).
      try {
        const [threads, providers] = await Promise.all([
          api.listThreads().catch(() => []),
          api.listProviders().catch(() => []),
        ]);
        if (!cancelled) {
          const isEmpty = (!Array.isArray(threads) || threads.length === 0) &&
                          (!Array.isArray(providers) || providers.length === 0);
          setShowOnboarding(isEmpty);
        }
      } catch {
        if (!cancelled) setShowOnboarding(false);
      }
    };
    check();
    return () => { cancelled = true; };
  }, []);

  // Ctrl+Shift+O resets onboarding / walkthrough state for development.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.ctrlKey && e.shiftKey && e.key === 'O') {
        localStorage.removeItem('onboarding_done');
        localStorage.removeItem('walkthrough_done');
        window.location.reload();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  // Wait for the check to complete before rendering.
  if (showOnboarding === null) return null;

  return (
    <Provider store={store}>
      <PersistGate loading={null} persistor={persistor}>
        <I18nProvider>
          <AppProvider>
            {showOnboarding && <OnboardingWizard onComplete={handleComplete} />}
            <KeyringConsentOverlay />
            <Walkthrough />
            <AppRouter />
          </AppProvider>
        </I18nProvider>
      </PersistGate>
    </Provider>
  );
}
