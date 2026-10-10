import { useEffect, useRef } from 'react';
import { EventsOn } from '../../../wailsjs/runtime/runtime';
import { useApp } from '../../state/AppContext';
import type { Toast } from '../../state/AppContext';
import { useT } from '../../lib/i18n/I18nContext';
import { Events } from '../../lib/events';

interface BackendNotification {
  id: string;
  kind: string;
  title: string;
  body: string;
  created_at: string;
}

// Maps the notifications.Kind values emitted by the backend to toast severities.
function toastKind(kind: string): Toast['kind'] {
  switch (kind) {
    case 'system_alert':
    case 'approval_needed':
      return 'warning';
    case 'reminder':
    case 'inbound_message':
    case 'mention':
    case 'cron_result':
      return 'info';
    default:
      return 'info';
  }
}

// NotificationBridge forwards backend notifications (the notifications.Bus) to
// the app's toast layer. The backend persists every notification and emits
// "notification:new"; without a listener they were written but never shown.
export function NotificationBridge() {
  const { actions } = useApp();
  const { t } = useT();

  // The context value (and therefore `actions`) changes whenever app state
  // changes, so depending on it would tear the listener down and re-add it on
  // every toast; notifications arriving in that gap would be dropped. Hold the
  // latest callback in a ref and register the listener exactly once. `t` is
  // held the same way so a locale switch reaches the fallback title.
  const addToastRef = useRef(actions.addToast);
  addToastRef.current = actions.addToast;
  const tRef = useRef(t);
  tRef.current = t;

  useEffect(() => {
    const off = EventsOn(Events.NotificationNew, (n: BackendNotification) => {
      if (!n) return;
      addToastRef.current(toastKind(n.kind), n.title || tRef.current('notifications.untitled'), n.body || '');
    });
    return () => { off?.(); };
  }, []);

  return null;
}
