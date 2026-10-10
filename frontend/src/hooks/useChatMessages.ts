import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useSelector, useDispatch, shallowEqual } from 'react-redux';
import type { RootState } from '../store';
import { addMessage } from '../store/chatSlice';
import { setThreads } from '../store/threadSlice';
import {
  toolCallStarted, toolCallCompleted, toolCallFailed, commitToolCalls,
  appendStreamToken, commitStreaming,
} from '../store/chatRuntimeSlice';
import * as api from '../services/wails';
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime';
import { useApp } from '../state/AppContext';
import { useT } from '../lib/i18n/I18nContext';
import { CHAT_EVENTS, Events } from '../lib/events';

const EMPTY_MSGS: import('../services/wails').MessageRecord[] = [];
let msgCounter = Date.now();
let toolCallCounter = 0;

// Active Wails event listeners that must survive across sendMessage calls
// but be cleaned up when the component unmounts.
const activeCleanups = new Set<() => void>();

function cleanupAll() {
  for (const fn of activeCleanups) fn();
  activeCleanups.clear();
  // EventsOff needs at least one name, then the rest variadically.
  EventsOff(CHAT_EVENTS[0], ...CHAT_EVENTS.slice(1));
}

// The turn currently in flight. finishStopped finalizes it locally as a
// user-stopped turn (never as an error).
interface ActiveTurn {
  threadId: string;
  finishStopped: () => void;
}

export function useChatMessages() {
  const dispatch = useDispatch();
  const activeThreadId = useSelector((s: RootState) => s.thread.activeThreadId);
  const { actions } = useApp();
  const { t } = useT();

  // Clean up all listeners when the component unmounts (user navigates away
  // from chat). This prevents zombie listeners from accumulating across
  // page navigations, which was the root cause of duplicate messages.
  useEffect(() => {
    return () => cleanupAll();
  }, []);

  // Clean up previous turn's listeners (normal turn completion) but do NOT
  // clear the module-level Set — the useEffect cleanup handles unmount.
  const turnCleanupRef = useRef<(() => void) | null>(null);
  const activeTurnRef = useRef<ActiveTurn | null>(null);
  const cancellingRef = useRef(false);
  // Thread with a turn in flight. Drives isStreaming even before the first
  // token arrives, so the Stop control is available for the whole turn.
  const [inFlightThreadId, setInFlightThreadId] = useState<string | null>(null);
  const [isCancelling, setIsCancelling] = useState(false);

  const sendMessage = useCallback(async (content: string) => {
    if (!activeThreadId) return;

    // Clean up previous turn's listeners (normal completion / error).
    if (turnCleanupRef.current) {
      turnCleanupRef.current();
      turnCleanupRef.current = null;
    }

    const threadId = activeThreadId;
    const userMsg = { id: ++msgCounter, role: 'user', content, created_at: new Date().toISOString() };
    dispatch(addMessage({ threadId, message: userMsg }));

    let streamingContent = '';
    // Set by chat:done / chat:error / stop. Suppresses late promise rejection
    // handling (and late events) once the turn has been finalized.
    let turnFinished = false;
    const activeToolCallIds: string[] = [];  // Stack of tool_call IDs for tool_result correlation

    const unsubs: (() => void)[] = [];

    const turnCleanup = () => {
      for (const unsub of unsubs) unsub();
      activeCleanups.delete(turnCleanup);
    };

    // Stop path: tear the listeners down first so a late chat:done/chat:error
    // from the cancelled backend turn cannot commit a second message.
    const finishStopped = () => {
      if (turnFinished) return;
      turnFinished = true;
      turnCleanup();
      // Tool calls that never produced a result would spin forever; record
      // them as failed-by-cancel so the timeline settles.
      for (const callId of activeToolCallIds) {
        dispatch(toolCallFailed({ threadId, id: callId, error: 'Stopped by user' }));
      }
      const asstMsgId = ++msgCounter;
      const asstMsg = {
        id: asstMsgId, role: 'assistant',
        content: streamingContent, created_at: new Date().toISOString(),
        stopped: true,
      };
      dispatch(commitToolCalls({ threadId, messageId: asstMsgId }));
      dispatch(addMessage({ threadId, message: asstMsg }));
      dispatch(commitStreaming(threadId));
      setInFlightThreadId(current => (current === threadId ? null : current));
      if (activeTurnRef.current === turn) activeTurnRef.current = null;
      api.listThreads().then(threads => dispatch(setThreads(threads))).catch(() => {});
    };

    const turn: ActiveTurn = { threadId, finishStopped };

    // Token streaming.
    unsubs.push(EventsOn(Events.ChatToken, (data: any) => {
      if (data.threadId !== threadId) return;
      streamingContent += data.content || '';
      dispatch(appendStreamToken({ threadId, token: data.content || '' }));
    }));

    // Tool call started.
    unsubs.push(EventsOn(Events.ChatToolCall, (data: any) => {
      if (data.threadId !== threadId) return;
      const id = `tc-${++toolCallCounter}`;
      activeToolCallIds.push(id);
      dispatch(toolCallStarted({ threadId, id, name: data.content || '', args: data.args || '' }));
    }));

    // Tool result. The Go StreamEvent carries Error for a failed tool call, so
    // a non-empty error must render as a failure, not as a success.
    unsubs.push(EventsOn(Events.ChatToolResult, (data: any) => {
      if (data.threadId !== threadId) return;
      const id = activeToolCallIds.pop() || `tc-${toolCallCounter}`;
      const error = typeof data.error === 'string' ? data.error : '';
      if (error) {
        dispatch(toolCallFailed({ threadId, id, error }));
      } else {
        dispatch(toolCallCompleted({ threadId, id, output: data.content || '' }));
      }
    }));

    // Turn complete.
    unsubs.push(EventsOn(Events.ChatDone, (data: any) => {
      if (data.threadId !== threadId || turnFinished) return;
      turnFinished = true;
      const finalContent = streamingContent || data.content || '';
      const asstMsgId = ++msgCounter;
      const asstMsg = {
        id: asstMsgId, role: 'assistant',
        content: finalContent, created_at: new Date().toISOString(),
      };
      dispatch(commitToolCalls({ threadId, messageId: asstMsgId }));
      dispatch(addMessage({ threadId, message: asstMsg }));
      dispatch(commitStreaming(threadId));
      setInFlightThreadId(current => (current === threadId ? null : current));
      if (activeTurnRef.current === turn) activeTurnRef.current = null;
      api.listThreads().then(threads => dispatch(setThreads(threads))).catch(() => {});
      // Clean up this turn's listeners.
      turnCleanupRef.current?.();
      turnCleanupRef.current = null;
    }));

    // Turn failed: commit the streamed slot as a failed assistant message
    // rather than a normal completed turn. Prefer the explicit error field;
    // Go duplicates the same text in content.
    unsubs.push(EventsOn(Events.ChatError, (data: any) => {
      if (data.threadId !== threadId || turnFinished) return;
      turnFinished = true;
      const errorText = String(data.error || data.content || 'unknown error');
      // Commit pending tool calls so they show in history.
      const errMsgId = ++msgCounter;
      dispatch(commitToolCalls({ threadId, messageId: errMsgId }));
      const errMsg = {
        id: errMsgId, role: 'assistant',
        content: `Error: ${errorText}`,
        created_at: new Date().toISOString(),
      };
      dispatch(addMessage({ threadId, message: errMsg }));
      dispatch(commitStreaming(threadId));
      setInFlightThreadId(current => (current === threadId ? null : current));
      if (activeTurnRef.current === turn) activeTurnRef.current = null;
      actions.addToast('error', t('errors.chatTurnFailed'), errorText);
      turnCleanupRef.current?.();
      turnCleanupRef.current = null;
    }));

    // Register the turn before awaiting so the Stop control can reach it.
    activeCleanups.add(turnCleanup);
    turnCleanupRef.current = turnCleanup;
    activeTurnRef.current = turn;
    setInFlightThreadId(threadId);

    try {
      await api.streamChatMessage({ threadId, message: content });
    } catch (e) {
      // A user-stopped turn already committed its message; the rejection of
      // the cancelled stream is expected and must not surface as an error.
      if (turnFinished) return;
      turnFinished = true;
      const errorText = String(e);
      const errMsg = {
        id: ++msgCounter, role: 'assistant',
        content: `Error: ${errorText}`,
        created_at: new Date().toISOString(),
      };
      dispatch(addMessage({ threadId, message: errMsg }));
      dispatch(commitStreaming(threadId));
      setInFlightThreadId(current => (current === threadId ? null : current));
      if (activeTurnRef.current === turn) activeTurnRef.current = null;
      actions.addToast('error', t('errors.chatTurnFailed'), errorText);
      turnCleanupRef.current?.();
      turnCleanupRef.current = null;
    }
  }, [dispatch, activeThreadId, actions]);

  // Stop the in-flight turn: cancel it on the backend, then finalize the
  // partial assistant message locally as stopped. Cancel is best-effort — the
  // turn is finalized even if the backend call fails, so the UI cannot hang.
  const cancelTurn = useCallback(async () => {
    const turn = activeTurnRef.current;
    if (!turn || cancellingRef.current) return;
    cancellingRef.current = true;
    setIsCancelling(true);
    try {
      await api.cancelMessage(turn.threadId);
    } catch (e) {
      console.debug('CancelMessage failed; finalizing the turn locally', e);
    } finally {
      turn.finishStopped();
      cancellingRef.current = false;
      setIsCancelling(false);
    }
  }, []);

  const messagesByThread = useSelector((s: RootState) => s.chat.messages, shallowEqual);
  const streamingByThread = useSelector((s: RootState) => s.chatRuntime.streaming, shallowEqual);
  const toolCallsByThread = useSelector((s: RootState) => s.chatRuntime.toolCalls, shallowEqual);
  const messages = useMemo(() => {
    if (!activeThreadId) return EMPTY_MSGS;
    return messagesByThread[activeThreadId] || EMPTY_MSGS;
  }, [activeThreadId, messagesByThread]);
  const streamingContent = activeThreadId ? (streamingByThread[activeThreadId] || '') : '';
  const hasActiveToolCalls = activeThreadId
    ? (toolCallsByThread[activeThreadId]?.some(tc => tc.status === 'pending' || tc.status === 'running') ?? false)
    : false;
  const turnInFlight = activeThreadId !== null && inFlightThreadId === activeThreadId;
  const isStreaming = turnInFlight || streamingContent.length > 0 || hasActiveToolCalls;

  return { messages, streamingContent, isStreaming, isCancelling, cancelTurn, sendMessage };
}
