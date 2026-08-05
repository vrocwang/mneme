import { Component } from 'react';
import type { ReactNode } from 'react';
import { useT } from '../../lib/i18n/I18nContext';

interface Props {
  children: ReactNode;
}

interface State {
  error: Error | null;
}

// Wrapper that provides i18n to the class component.
function ErrorFallback({ error, onRetry }: { error: Error; onRetry: () => void }) {
  const { t } = useT();
  return (
    <div className="flex items-center justify-center h-full bg-surface">
      <div className="text-center space-y-4 p-8">
        <div className="w-14 h-14 mx-auto rounded-2xl bg-coral-500/20 flex items-center justify-center">
          <svg className="w-7 h-7 text-coral-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-2.694-.833-3.464 0L3.34 16.5c-.77.833.192 2.5 1.732 2.5z" />
          </svg>
        </div>
        <h2 className="text-lg font-semibold text-white/80">{t('errors.somethingWrong')}</h2>
        <p className="text-sm text-white/40 max-w-sm">
          {error.message || 'An unexpected error occurred.'}
        </p>
        <button className="btn-primary" onClick={onRetry}>
          {t('errors.tryAgain')}
        </button>
      </div>
    </div>
  );
}

export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error) {
    console.error('[ErrorBoundary] caught render error:', error);
  }

  render() {
    if (this.state.error) {
      return <ErrorFallback error={this.state.error} onRetry={() => this.setState({ error: null })} />;
    }
    return this.props.children;
  }
}
