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
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { useApp } from '../state/AppContext';
import { useT } from '../lib/i18n/I18nContext';
import { Events } from '../lib/events';

const EMPTY_MSGS: import('../services/wails').MessageRecord[] = [];
let msgCounter = Date.now();
let toolCallCounter = 0;
// Identity handed to every turn: a finishing turn writes thread-keyed state
// only while it still owns it.
let turnCounter = 0;

// A per-turn teardown, keyed by turn id. EventsOn returns a per-listener
// unsubscribe; EventsOff(event) would remove every listener for that event,
// including other hook instances' ones.
const turnCleanups = new Map<number, () => void>();

// cleanupAll runs when the chat view unmounts (a route change). It must NOT tear
// down a turn that is still in flight: those listeners live at module scope and
// are what finalize the turn (commit the reply, clear the in-flight flag). The
// window between an unmount and chat:done is exactly when a reply would
// otherwise be lost, leaving the composer locked until the user pressed Stop.
// Finished turns are already removed by their own teardown, so anything left
// here for a live turn is kept and torn down when that turn completes.
function cleanupAll() {
  for (const [id, fn] of turnCleanups) {
    if (inFlightTurns.has(id)) continue;
    fn();
    turnCleanups.delete(id);
  }
}

// A turn in flight. finishStopped finalizes it locally as a user-stopped turn
// (never as an error).
interface ActiveTurn {
  id: number;
  threadId: string;
  finishStopped: () => void;
}

// In-flight turns keyed by turn id, at module scope: ChatView and the Composer
// remount on thread change, so "this thread has a turn in flight" must not live
// in component state alone.
const inFlightTurns = new Map<number, ActiveTurn>();
const inFlightListeners = new Set<() => void>();
// threadId -> id of the turn that owns the thread's streaming slot. A stopped
// turn can still be awaiting its cancel IPC round trip while the next turn for
// the same thread starts, so only the owner may commit or clear the slot.
const streamOwnerByThread = new Map<string, number>();

function notifyInFlightChange() {
  for (const listener of inFlightListeners) listener();
}

function threadHasTurnInFlight(threadId: string): boolean {
  for (const turn of inFlightTurns.values()) {
    if (turn.threadId === threadId) return true;
  }
  return false;
}

// Newest in-flight turn for a thread: the one Stop and a new send must act on.
function latestTurnForThread(threadId: string): ActiveTurn | null {
  let latest: ActiveTurn | null = null;
  for (const turn of inFlightTurns.values()) {
    if (turn.threadId === threadId && (!latest || turn.id > latest.id)) latest = turn;
  }
  return latest;
}

function removeInFlight(turn: ActiveTurn) {
  if (inFlightTurns.delete(turn.id)) notifyInFlightChange();
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

  // Bumped whenever a turn starts or ends. isStreaming reads the module-level
  // registry, which outlives this hook instance across remounts.
  const [inFlightVersion, setInFlightVersion] = useState(0);
  useEffect(() => {
    const bump = () => setInFlightVersion(v => v + 1);
    inFlightListeners.add(bump);
    return () => { inFlightListeners.delete(bump); };
  }, []);

  // Cleanup slots keyed by turn id: tearing down one turn's listeners must
  // never tear down another turn's. A single slot lost the other thread's live
  // listeners as soon as a second turn started.
  const cancellingRef = useRef(false);
  const [isCancelling, setIsCancelling] = useState(false);

  const sendMessage = useCallback(async (content: string) => {
    if (!activeThreadId) return;

    const threadId = activeThreadId;

    // A turn for this thread may still be in flight (the user stopped it and
    // sent again before the cancel IPC returned, or the view remounted).
    // Finalize it first so the two turns cannot interleave thread-keyed writes.
    latestTurnForThread(threadId)?.finishStopped();

    const userMsg = { id: ++msgCounter, role: 'user', content, created_at: new Date().toISOString() };
    dispatch(addMessage({ threadId, message: userMsg }));

    let streamingContent = '';
    // Set by chat:done / chat:error / stop. Suppresses late promise rejection
    // handling (and late events) once the turn has been finalized.
    let turnFinished = false;
    const activeToolCallIds: string[] = [];  // Stack of tool_call IDs for tool_result correlation

    const unsubs: (() => void)[] = [];

    // Per-turn teardown slot. The map entry is the idempotency guard, so a
    // turn tears down only its own listeners and never another turn's.
    const turnCleanup = () => {
      if (!turnCleanups.delete(turn.id)) return;
      for (const unsub of unsubs) unsub();
    };

    // Pending tool calls and the streaming slot are keyed by thread, not by
    // turn, so only the owning turn may commit them: a newer turn's tokens and
    // tool calls must survive an older turn's finalization.
    const commitThreadState = (messageId: number) => {
      if (streamOwnerByThread.get(threadId) !== turn.id) return;
      dispatch(commitToolCalls({ threadId, messageId }));
      dispatch(commitStreaming(threadId));
      streamOwnerByThread.delete(threadId);
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
      commitThreadState(asstMsgId);
      dispatch(addMessage({ threadId, message: asstMsg }));
      removeInFlight(turn);
      api.listThreads().then(threads => dispatch(setThreads(threads))).catch(() => {});
    };

    const turn: ActiveTurn = { id: ++turnCounter, threadId, finishStopped };

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
      commitThreadState(asstMsgId);
      dispatch(addMessage({ threadId, message: asstMsg }));
      removeInFlight(turn);
      api.listThreads().then(threads => dispatch(setThreads(threads))).catch(() => {});
      // Clean up this turn's listeners, never another turn's.
      turnCleanup();
    }));

    // Turn failed: commit the streamed slot as a failed assistant message
    // rather than a normal completed turn. Prefer the explicit error field;
    // Go duplicates the same text in content.
    unsubs.push(EventsOn(Events.ChatError, (data: any) => {
      if (data.threadId !== threadId || turnFinished) return;
      turnFinished = true;
      const errorText = String(data.error || data.content || 'unknown error');
      const errMsgId = ++msgCounter;
      const errMsg = {
        id: errMsgId, role: 'assistant',
        content: `Error: ${errorText}`,
        created_at: new Date().toISOString(),
      };
      // Commit pending tool calls so they show in history.
      commitThreadState(errMsgId);
      dispatch(addMessage({ threadId, message: errMsg }));
      removeInFlight(turn);
      actions.addToast('error', t('errors.chatTurnFailed'), errorText);
      turnCleanup();
    }));

    // Register the turn before awaiting so the Stop control can reach it.
    turnCleanups.set(turn.id, turnCleanup);
    inFlightTurns.set(turn.id, turn);
    streamOwnerByThread.set(threadId, turn.id);
    notifyInFlightChange();

    try {
      await api.streamChatMessage({ threadId, message: content });
    } catch (e) {
      // A user-stopped turn already committed its message; the rejection of
      // the cancelled stream is expected and must not surface as an error.
      if (turnFinished) return;
      turnFinished = true;
      const errorText = String(e);
      const errMsgId = ++msgCounter;
      const errMsg = {
        id: errMsgId, role: 'assistant',
        content: `Error: ${errorText}`,
        created_at: new Date().toISOString(),
      };
      commitThreadState(errMsgId);
      dispatch(addMessage({ threadId, message: errMsg }));
      removeInFlight(turn);
      actions.addToast('error', t('errors.chatTurnFailed'), errorText);
      turnCleanup();
    }
  }, [dispatch, activeThreadId, actions, t]);

  // Stop the in-flight turn for the active thread: cancel it on the backend,
  // then finalize the partial assistant message locally as stopped. Cancel is
  // best-effort — the turn is finalized even if the backend call fails, so the
  // UI cannot hang.
  const cancelTurn = useCallback(async () => {
    const threadId = activeThreadId;
    if (!threadId || cancellingRef.current) return;
    const turn = latestTurnForThread(threadId);
    if (!turn) return;
    cancellingRef.current = true;
    setIsCancelling(true);
    try {
      await api.cancelMessage(threadId);
    } catch (e) {
      console.debug('CancelMessage failed; finalizing the turn locally', e);
    } finally {
      turn.finishStopped();
      cancellingRef.current = false;
      setIsCancelling(false);
    }
  }, [activeThreadId]);

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
  // Any turn in flight for the active thread — not just this hook instance's
  // last one, and not just while the Composer keeps its local sending flag.
  const turnInFlight = useMemo(
    () => activeThreadId !== null && threadHasTurnInFlight(activeThreadId),
    [activeThreadId, inFlightVersion],
  );
  const isStreaming = turnInFlight || streamingContent.length > 0 || hasActiveToolCalls;

  return { messages, streamingContent, isStreaming, turnInFlight, isCancelling, cancelTurn, sendMessage };
}
