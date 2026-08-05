import { createSlice, PayloadAction } from '@reduxjs/toolkit';
import type { ThreadSummary } from '../services/wails';

interface ThreadState {
  threads: ThreadSummary[];
  activeThreadId: string | null;
}

const initialState: ThreadState = { threads: [], activeThreadId: null };

const threadSlice = createSlice({
  name: 'thread',
  initialState,
  reducers: {
    setThreads(state, action: PayloadAction<ThreadSummary[]>) { state.threads = action.payload; },
    addThread(state, action: PayloadAction<ThreadSummary>) { state.threads.unshift(action.payload); },
    removeThread(state, action: PayloadAction<string>) {
      state.threads = state.threads.filter(t => t.id !== action.payload);
    },
    setActiveThread(state, action: PayloadAction<string | null>) { state.activeThreadId = action.payload; },
  },
});

export const { setThreads, addThread, removeThread, setActiveThread } = threadSlice.actions;
export default threadSlice.reducer;
