import { useCallback, useEffect } from 'react';
import { useSelector, useDispatch } from 'react-redux';
import type { RootState } from '../store';
import { setPending, removePending } from '../store/approvalSlice';
import * as api from '../services/wails';
import type { PendingApproval } from '../services/wails';
import { useApp } from '../state/AppContext';
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime';

const EMPTY_APPROVALS: PendingApproval[] = [];

export function useApprovals() {
  const dispatch = useDispatch();
  const pendingApprovals = useSelector((s: RootState) => s.approval.pending) || EMPTY_APPROVALS;
  const { actions } = useApp();

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
      actions.addToast('success', 'Decision recorded',
        decision === 'approve_always' ? 'Tool permanently allowed' : decision === 'approve_once' ? 'Approved once' : 'Denied');
    } catch (e) {
      actions.addToast('error', 'Failed to decide', String(e));
    }
  }, [dispatch, actions]);

  // Poll on mount and every 10s as fallback.
  useEffect(() => {
    refreshApprovals();
    const interval = setInterval(refreshApprovals, 10000);
    return () => clearInterval(interval);
  }, [refreshApprovals]);

  // Listen for real-time approval events pushed from the backend via Wails.
  useEffect(() => {
    const onApproval = () => { refreshApprovals(); };
    EventsOn('approval:approval.requested', onApproval);
    EventsOn('approval:approval.decided', onApproval);
    return () => {
      EventsOff('approval:approval.requested');
      EventsOff('approval:approval.decided');
    };
  }, [refreshApprovals]);

  return { pendingApprovals, refreshApprovals, decideApproval };
}
