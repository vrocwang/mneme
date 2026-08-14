import { useCallback, useEffect, useMemo, useRef } from 'react';
import { useSelector, useDispatch, shallowEqual } from 'react-redux';
import type { RootState } from '../store';
import { addMessage } from '../store/chatSlice';
import { setThreads } from '../store/threadSlice';
import {
  toolCallStarted, toolCallCompleted, toolCallFailed, commitToolCalls,
  appendStreamToken, appendThinkingToken, commitStreaming,
} from '../store/chatRuntimeSlice';
import * as api from '../services/wails';
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime';
import { useApp } from '../state/AppContext';

const EMPTY_MSGS: import('../services/wails').MessageRecord[] = [];
let msgCounter = Date.now();
let toolCallCounter = 0;

// Active Wails event listeners that must survive across sendMessage calls
// but be cleaned up when the component unmounts.
const activeCleanups = new Set<() => void>();

const CHAT_EVENTS = [
  'chat:token', 'chat:tool_call', 'chat:tool_result',
  'chat:done', 'chat:error', 'chat:thinking',
] as const;

function cleanupAll() {
  for (const fn of activeCleanups) fn();
  activeCleanups.clear();
  EventsOff(...CHAT_EVENTS);
}

export function useChatMessages() {
  const dispatch = useDispatch();
  const activeThreadId = useSelector((s: RootState) => s.thread.activeThreadId);
  const { actions } = useApp();

  // Clean up all listeners when the component unmounts (user navigates away
  // from chat). This prevents zombie listeners from accumulating across
  // page navigations, which was the root cause of duplicate messages.
  useEffect(() => {
    return () => cleanupAll();
  }, []);

  // Clean up previous turn's listeners (normal turn completion) but do NOT
  // clear the module-level Set — the useEffect cleanup handles unmount.
  const turnCleanupRef = useRef<(() => void) | null>(null);

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
    const activeToolCallIds: string[] = [];  // Stack of tool_call IDs for tool_result correlation

    const unsubs: (() => void)[] = [];

    // Token streaming.
    unsubs.push(EventsOn('chat:token', (data: any) => {
      if (data.threadId !== threadId) return;
      streamingContent += data.content || '';
      dispatch(appendStreamToken({ threadId, token: data.content || '' }));
    }));

    // Thinking streaming.
    unsubs.push(EventsOn('chat:thinking', (data: any) => {
      if (data.threadId !== threadId) return;
      dispatch(appendThinkingToken({ threadId, token: data.content || '' }));
    }));

    // Tool call started.
    unsubs.push(EventsOn('chat:tool_call', (data: any) => {
      if (data.threadId !== threadId) return;
      const id = `tc-${++toolCallCounter}`;
      activeToolCallIds.push(id);
      dispatch(toolCallStarted({ threadId, id, name: data.content || '', args: data.args || '' }));
    }));

    // Tool result.
    unsubs.push(EventsOn('chat:tool_result', (data: any) => {
      if (data.threadId !== threadId) return;
      const id = activeToolCallIds.pop() || `tc-${toolCallCounter}`;
      if (data.error) {
        dispatch(toolCallFailed({ threadId, id, error: data.error }));
      } else {
        dispatch(toolCallCompleted({ threadId, id, output: data.content || '' }));
      }
    }));

    // Turn complete.
    unsubs.push(EventsOn('chat:done', (data: any) => {
      if (data.threadId !== threadId) return;
      const finalContent = streamingContent || data.content || '';
      const asstMsgId = ++msgCounter;
      const asstMsg = {
        id: asstMsgId, role: 'assistant',
        content: finalContent, created_at: new Date().toISOString(),
      };
      dispatch(commitToolCalls({ threadId, messageId: asstMsgId }));
      dispatch(addMessage({ threadId, message: asstMsg }));
      dispatch(commitStreaming(threadId));
      api.listThreads().then(threads => dispatch(setThreads(threads))).catch(() => {});
      // Clean up this turn's listeners.
      turnCleanupRef.current?.();
      turnCleanupRef.current = null;
    }));

    // Error.
    unsubs.push(EventsOn('chat:error', (data: any) => {
      if (data.threadId !== threadId) return;
      // Commit pending tool calls so they show in history.
      const errMsgId = ++msgCounter;
      dispatch(commitToolCalls({ threadId, messageId: errMsgId }));
      const errMsg = {
        id: errMsgId, role: 'assistant',
        content: `Error: ${data.error || data.content || 'unknown error'}`,
        created_at: new Date().toISOString(),
      };
      dispatch(addMessage({ threadId, message: errMsg }));
      dispatch(commitStreaming(threadId));
      turnCleanupRef.current?.();
      turnCleanupRef.current = null;
    }));

    const turnCleanup = () => {
      for (const unsub of unsubs) unsub();
      activeCleanups.delete(turnCleanup);
    };
    activeCleanups.add(turnCleanup);
    turnCleanupRef.current = turnCleanup;

    try {
      await api.streamChatMessage({ threadId, message: content });
    } catch (e) {
      const errMsg = {
        id: ++msgCounter, role: 'assistant',
        content: `Error: ${e}`,
        created_at: new Date().toISOString(),
      };
      dispatch(addMessage({ threadId, message: errMsg }));
      dispatch(commitStreaming(threadId));
      turnCleanupRef.current?.();
      turnCleanupRef.current = null;
    }
  }, [dispatch, activeThreadId]);

  const messagesByThread = useSelector((s: RootState) => s.chat.messages, shallowEqual);
  const streamingByThread = useSelector((s: RootState) => s.chatRuntime.streaming, shallowEqual);
  const streamingThinkingByThread = useSelector((s: RootState) => s.chatRuntime.streamingThinking, shallowEqual);
  const toolCallsByThread = useSelector((s: RootState) => s.chatRuntime.toolCalls, shallowEqual);
  const messages = useMemo(() => {
    if (!activeThreadId) return EMPTY_MSGS;
    return messagesByThread[activeThreadId] || EMPTY_MSGS;
  }, [activeThreadId, messagesByThread]);
  const streamingContent = activeThreadId ? (streamingByThread[activeThreadId] || '') : '';
  const streamingThinking = activeThreadId ? (streamingThinkingByThread[activeThreadId] || '') : '';
  const hasActiveToolCalls = activeThreadId
    ? (toolCallsByThread[activeThreadId]?.some(tc => tc.status === 'pending' || tc.status === 'running') ?? false)
    : false;
  const isStreaming = streamingContent.length > 0 || streamingThinking.length > 0 || hasActiveToolCalls;

  return { messages, streamingContent, streamingThinking, isStreaming, sendMessage };
}
