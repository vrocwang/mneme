import { createSlice, PayloadAction } from '@reduxjs/toolkit';
import type { PendingApproval } from '../services/wails';

interface ApprovalState {
  pending: PendingApproval[];
}

const approvalSlice = createSlice({
  name: 'approval',
  initialState: { pending: [] } as ApprovalState,
  reducers: {
    setPending(state, action: PayloadAction<PendingApproval[]>) {
      state.pending = action.payload;
    },
    removePending(state, action: PayloadAction<string>) {
      state.pending = state.pending.filter(r => r.id !== action.payload);
    },
  },
});

export const { setPending, removePending } = approvalSlice.actions;
export default approvalSlice.reducer;
