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

// ── Subagent ──────────────────────────────────────────────────────────────

export interface SubagentEntry {
  id: string;
  agentType: string;
  task: string;
  status: 'running' | 'completed' | 'failed';
  output?: string;
  error?: string;
  startedAt: string;
}

// ── State ───────────────────────────────────────────────────────────────

interface ChatRuntimeState {
  toolCalls: Record<string, ToolCallEntry[]>;           // threadId → pending tool calls (current turn)
  toolCallsByMsg: Record<number, ToolCallEntry[]>;      // messageId → tool calls for that assistant message
  subagents: Record<string, SubagentEntry[]>;            // threadId → subagents
  streaming: Record<string, string>;                     // threadId → streaming content
  streamingThinking: Record<string, string>;              // threadId → streaming thinking content
}

const initialState: ChatRuntimeState = {
  toolCalls: {},
  toolCallsByMsg: {},
  subagents: {},
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

    // ── Subagents ────────────────────────────────────────────────────
    subagentSpawned(state, action: PayloadAction<{ threadId: string; id: string; agentType: string; task: string }>) {
      const { threadId, id, agentType, task } = action.payload;
      if (!state.subagents[threadId]) state.subagents[threadId] = [];
      state.subagents[threadId].push({
        id, agentType, task,
        status: 'running',
        startedAt: new Date().toISOString(),
      });
    },
    subagentCompleted(state, action: PayloadAction<{ threadId: string; id: string; output?: string }>) {
      const agents = state.subagents[action.payload.threadId];
      const entry = agents?.find(a => a.id === action.payload.id);
      if (entry) {
        entry.status = 'completed';
        entry.output = action.payload.output;
      }
    },
    subagentFailed(state, action: PayloadAction<{ threadId: string; id: string; error?: string }>) {
      const agents = state.subagents[action.payload.threadId];
      const entry = agents?.find(a => a.id === action.payload.id);
      if (entry) {
        entry.status = 'failed';
        entry.error = action.payload.error;
      }
    },
  },
});

export const {
  toolCallStarted, toolCallCompleted, toolCallFailed, commitToolCalls,
  appendStreamToken, appendThinkingToken, commitStreaming,
  subagentSpawned, subagentCompleted, subagentFailed,
} = chatRuntimeSlice.actions;

export default chatRuntimeSlice.reducer;
