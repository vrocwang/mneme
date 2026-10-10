import { useCallback, useEffect } from 'react';
import { useSelector, useDispatch } from 'react-redux';
import type { RootState } from '../store';
import { setThreads, addThread, removeThread, setActiveThread } from '../store/threadSlice';
import { setMessages } from '../store/chatSlice';
import * as api from '../services/wails';
import type { ThreadSummary } from '../services/wails';
import { useApp } from '../state/AppContext';
import { useT } from '../lib/i18n/I18nContext';

const EMPTY_THREADS: ThreadSummary[] = [];

export function useThreads() {
  const dispatch = useDispatch();
  const threads = useSelector((s: RootState) => s.thread.threads) || EMPTY_THREADS;
  const activeThreadId = useSelector((s: RootState) => s.thread.activeThreadId);
  const { actions } = useApp();
  const { t } = useT();

  const loadThreads = useCallback(async () => {
    try {
      const list = await api.listThreads();
      // The backend list is authoritative, including when it is empty:
      // deleted or stale persisted threads must not survive a reload.
      dispatch(setThreads(list));
    } catch {
      // Backend unavailable — keep the last known list.
    }
  }, [dispatch]);

  const selectThread = useCallback(async (id: string) => {
    dispatch(setActiveThread(id));
    try {
      const msgs = await api.getThreadMessages(id);
      dispatch(setMessages({ threadId: id, messages: msgs }));
    } catch {
      // new thread may have no messages
    }
  }, [dispatch]);

  const createNewThread = useCallback(async (title?: string) => {
    try {
      // ThreadsRPC.CreateThread reports a failure as {"error": "..."} rather
      // than rejecting, so the reply is typed for both shapes.
      const thread = await api.createThread(title || '') as ThreadSummary & { error?: string };
      if (thread.error || !thread.id) {
        actions.addToast('error', t('errors.createThreadFailed'), thread.error || t('errors.invalidResponse'));
        return null;
      }
      dispatch(addThread(thread));
      dispatch(setActiveThread(thread.id));
      dispatch(setMessages({ threadId: thread.id, messages: [] }));
      return thread;
    } catch (e) {
      actions.addToast('error', t('errors.createThreadFailed'), String(e));
      return null;
    }
  }, [dispatch, actions, t]);

  const deleteSelectedThread = useCallback(async (id: string) => {
    try {
      await api.deleteThread(id);
      dispatch(removeThread(id));
      if (activeThreadId === id) {
        dispatch(setActiveThread(null));
      }
    } catch (e) {
      actions.addToast('error', t('errors.deleteThreadFailed'), String(e));
    }
  }, [dispatch, activeThreadId, actions, t]);

  useEffect(() => { loadThreads(); }, [loadThreads]);

  return { threads, activeThreadId, loadThreads, selectThread, createNewThread, deleteSelectedThread };
}
