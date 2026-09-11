import { useEffect, useRef } from 'react';
import { EventsOn } from '../../../wailsjs/runtime/runtime';
import { useApp } from '../../state/AppContext';

interface BackendNotification {
  id: string;
  kind: string;
  title: string;
  body: string;
  created_at: string;
}

// NotificationBridge forwards backend notifications (the notifications.Bus) to
// the app's toast layer. The backend persists every notification and emits
// "notification:new"; without a listener they were written but never shown.
export function NotificationBridge() {
  const { actions } = useApp();

  // The context value (and therefore `actions`) changes whenever app state
  // changes, so depending on it would tear the listener down and re-add it on
  // every toast; notifications arriving in that gap would be dropped. Hold the
  // latest callback in a ref and register the listener exactly once.
  const addToastRef = useRef(actions.addToast);
  addToastRef.current = actions.addToast;

  useEffect(() => {
    const off = EventsOn('notification:new', (n: BackendNotification) => {
      if (!n) return;
      const kind = n.kind === 'system_alert' ? 'warning' : 'info';
      addToastRef.current(kind, n.title || 'Notification', n.body || '');
    });
    return () => { off?.(); };
  }, []);

  return null;
}
