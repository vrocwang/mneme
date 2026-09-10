import { createSlice, PayloadAction } from '@reduxjs/toolkit';
import type { MessageRecord } from '../services/wails';

interface ChatState {
  messages: Record<string, MessageRecord[]>; // threadId → messages
}

const initialState: ChatState = {
  messages: {},
};

const chatSlice = createSlice({
  name: 'chat',
  initialState,
  reducers: {
    addMessage(state, action: PayloadAction<{ threadId: string; message: MessageRecord }>) {
      const { threadId, message } = action.payload;
      if (!state.messages[threadId]) state.messages[threadId] = [];
      state.messages[threadId].push(message);
    },
    setMessages(state, action: PayloadAction<{ threadId: string; messages: MessageRecord[] }>) {
      state.messages[action.payload.threadId] = action.payload.messages;
    },
  },
});

export const { addMessage, setMessages } = chatSlice.actions;
export default chatSlice.reducer;
