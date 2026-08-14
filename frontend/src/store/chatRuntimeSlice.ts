import { createSlice, PayloadAction } from '@reduxjs/toolkit';

// ── Tool call timeline ─────────────────────────────────────────────────

export interface ToolCallEntry {
  id: string;
  name: string;
  args: string;
  status: 'pending' | 'running' | 'done' | 'error';
  output?: string;
  error?: string;
  startedAt: string;
  finishedAt?: string;
}

// ── State ───────────────────────────────────────────────────────────────

interface ChatRuntimeState {
  toolCalls: Record<string, ToolCallEntry[]>;           // threadId → pending tool calls (current turn)
  toolCallsByMsg: Record<number, ToolCallEntry[]>;      // messageId → tool calls for that assistant message
  streaming: Record<string, string>;                     // threadId → streaming content
  streamingThinking: Record<string, string>;              // threadId → streaming thinking content
}

const initialState: ChatRuntimeState = {
  toolCalls: {},
  toolCallsByMsg: {},
  streaming: {},
  streamingThinking: {},
};

const chatRuntimeSlice = createSlice({
  name: 'chatRuntime',
  initialState,
  reducers: {
    // ── Tool calls ──────────────────────────────────────────────────
    toolCallStarted(state, action: PayloadAction<{ threadId: string; id: string; name: string; args: string }>) {
      const { threadId, id, name, args } = action.payload;
      if (!state.toolCalls[threadId]) state.toolCalls[threadId] = [];
      state.toolCalls[threadId].push({
        id, name, args,
        status: 'running',
        startedAt: new Date().toISOString(),
      });
    },
    toolCallCompleted(state, action: PayloadAction<{ threadId: string; id: string; output: string }>) {
      const calls = state.toolCalls[action.payload.threadId];
      const entry = calls?.find(c => c.id === action.payload.id);
      if (entry) {
        entry.status = 'done';
        entry.output = action.payload.output;
        entry.finishedAt = new Date().toISOString();
      }
    },
    toolCallFailed(state, action: PayloadAction<{ threadId: string; id: string; error: string }>) {
      const calls = state.toolCalls[action.payload.threadId];
      const entry = calls?.find(c => c.id === action.payload.id);
      if (entry) {
        entry.status = 'error';
        entry.error = action.payload.error;
        entry.finishedAt = new Date().toISOString();
      }
    },
    // Move pending tool calls for a thread to a message, then clear pending.
    commitToolCalls(state, action: PayloadAction<{ threadId: string; messageId: number }>) {
      const { threadId, messageId } = action.payload;
      const pending = state.toolCalls[threadId];
      if (pending && pending.length > 0) {
        state.toolCallsByMsg[messageId] = [...pending];
        state.toolCalls[threadId] = [];
      }
    },

    // ── Streaming ───────────────────────────────────────────────────
    appendStreamToken(state, action: PayloadAction<{ threadId: string; token: string }>) {
      const { threadId, token } = action.payload;
      state.streaming[threadId] = (state.streaming[threadId] || '') + token;
    },
    appendThinkingToken(state, action: PayloadAction<{ threadId: string; token: string }>) {
      const { threadId, token } = action.payload;
      state.streamingThinking[threadId] = (state.streamingThinking[threadId] || '') + token;
    },
    commitStreaming(state, action: PayloadAction<string>) {
      const threadId = action.payload;
      delete state.streaming[threadId];
      delete state.streamingThinking[threadId];
    },
  },
});

export const {
  toolCallStarted, toolCallCompleted, toolCallFailed, commitToolCalls,
  appendStreamToken, appendThinkingToken, commitStreaming,
} = chatRuntimeSlice.actions;

export default chatRuntimeSlice.reducer;
