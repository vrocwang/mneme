import { useCallback, useEffect } from 'react';
import { useSelector, useDispatch } from 'react-redux';
import type { RootState } from '../store';
import { setThreads, addThread, removeThread, setActiveThread } from '../store/threadSlice';
import { setMessages, setLoading } from '../store/chatSlice';
import * as api from '../services/wails';
import type { ThreadSummary } from '../services/wails';
import { useApp } from '../state/AppContext';

const EMPTY_THREADS: ThreadSummary[] = [];

export function useThreads() {
  const dispatch = useDispatch();
  const threads = useSelector((s: RootState) => s.thread.threads) || EMPTY_THREADS;
  const activeThreadId = useSelector((s: RootState) => s.thread.activeThreadId);
  const { actions } = useApp();

  const loadThreads = useCallback(async () => {
    try {
      const list = await api.listThreads();
      // Don't overwrite with empty — persisted threads may still be valid
      // while the backend store hasn't initialized yet.
      if (list.length > 0) {
        dispatch(setThreads(list));
      }
    } catch {
      // silently fail — no threads yet
    }
  }, [dispatch]);

  const selectThread = useCallback(async (id: string) => {
    dispatch(setActiveThread(id));
    dispatch(setLoading({ threadId: id, loading: true }));
    try {
      const msgs = await api.getThreadMessages(id);
      dispatch(setMessages({ threadId: id, messages: msgs }));
    } catch {
      // new thread may have no messages
    } finally {
      dispatch(setLoading({ threadId: id, loading: false }));
    }
  }, [dispatch]);

  const createNewThread = useCallback(async (title?: string) => {
    try {
      const thread = await api.createThread(title || '');
      if (!thread || !thread.id) {
        const errMsg = (thread as any)?.error || 'Invalid response from server';
        actions.addToast('error', 'Failed to create thread', errMsg);
        return null;
      }
      dispatch(addThread(thread));
      dispatch(setActiveThread(thread.id));
      dispatch(setMessages({ threadId: thread.id, messages: [] }));
      return thread;
    } catch (e) {
      actions.addToast('error', 'Failed to create thread', String(e));
      return null;
    }
  }, [dispatch, actions]);

  const deleteSelectedThread = useCallback(async (id: string) => {
    try {
      await api.deleteThread(id);
      dispatch(removeThread(id));
      if (activeThreadId === id) {
        dispatch(setActiveThread(null));
      }
    } catch (e) {
      actions.addToast('error', 'Failed to delete thread', String(e));
    }
  }, [dispatch, activeThreadId, actions]);

  useEffect(() => { loadThreads(); }, [loadThreads]);

  return { threads, activeThreadId, loadThreads, selectThread, createNewThread, deleteSelectedThread };
}
