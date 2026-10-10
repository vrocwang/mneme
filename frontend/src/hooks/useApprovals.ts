import { useCallback, useEffect } from 'react';
import { useSelector, useDispatch } from 'react-redux';
import type { RootState } from '../store';
import { setPending, removePending } from '../store/approvalSlice';
import * as api from '../services/wails';
import type { PendingApproval } from '../services/wails';
import { useApp } from '../state/AppContext';
import { useT } from '../lib/i18n/I18nContext';
import { Events } from '../lib/events';
import { APPROVAL_POLL_MS } from '../lib/timing';
import { EventsOn } from '../../wailsjs/runtime/runtime';

const EMPTY_APPROVALS: PendingApproval[] = [];

export function useApprovals() {
  const dispatch = useDispatch();
  const pendingApprovals = useSelector((s: RootState) => s.approval.pending) || EMPTY_APPROVALS;
  const { actions } = useApp();
  const { t } = useT();

  const refreshApprovals = useCallback(async () => {
    try {
      const list = await api.listPendingApprovals();
      dispatch(setPending(list));
    } catch {
      // approval system may not be initialized
    }
  }, [dispatch]);

  const decideApproval = useCallback(async (id: string, decision: 'approve_once' | 'approve_always' | 'deny') => {
    try {
      await api.decideApproval(id, decision);
      dispatch(removePending(id));
      actions.addToast('success', t('approval.decisionRecorded'),
        decision === 'approve_always' ? t('approval.toolAllowed') : decision === 'approve_once' ? t('approval.approvedOnce') : t('approval.denied'));
    } catch (e) {
      actions.addToast('error', t('approval.decideFailed'), String(e));
    }
  }, [dispatch, actions, t]);

  // Poll on mount and every APPROVAL_POLL_MS as fallback.
  useEffect(() => {
    refreshApprovals();
    const interval = setInterval(refreshApprovals, APPROVAL_POLL_MS);
    return () => clearInterval(interval);
  }, [refreshApprovals]);

  // Listen for real-time approval events pushed from the backend via Wails.
  // EventsOn returns a per-listener unsubscribe; EventsOff(event) would remove
  // every listener for that event, including the other hook instances' ones
  // (three components mount this hook concurrently).
  useEffect(() => {
    const onApproval = () => { refreshApprovals(); };
    const offRequested = EventsOn(Events.ApprovalRequested, onApproval);
    const offDecided = EventsOn(Events.ApprovalDecided, onApproval);
    return () => {
      offRequested();
      offDecided();
    };
  }, [refreshApprovals]);

  return { pendingApprovals, refreshApprovals, decideApproval };
}
