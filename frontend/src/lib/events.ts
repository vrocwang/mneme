// Names of the Wails events the Go backend emits. Every listener goes through
// these constants: a typo in a string literal is a silently dead subscription.

export const Events = {
  NotificationNew: 'notification:new',
  ApprovalRequested: 'approval:approval.requested',
  ApprovalDecided: 'approval:approval.decided',
  ChatToken: 'chat:token',
  ChatToolCall: 'chat:tool_call',
  ChatToolResult: 'chat:tool_result',
  ChatError: 'chat:error',
  ChatDone: 'chat:done',
} as const;

export type EventName = (typeof Events)[keyof typeof Events];

// Every event carrying a chat turn; they are subscribed and torn down together.
export const CHAT_EVENTS: readonly EventName[] = [
  Events.ChatToken,
  Events.ChatToolCall,
  Events.ChatToolResult,
  Events.ChatDone,
  Events.ChatError,
];
