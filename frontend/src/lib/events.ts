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
