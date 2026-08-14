import { useEffect, useRef } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useApp } from '../../state/AppContext';
import { useT } from '../../lib/i18n/I18nContext';
import { useThreads } from '../../hooks/useThreads';
import { useApprovals } from '../../hooks/useApprovals';
import { useChatMessages } from '../../hooks/useChatMessages';
import { useSelector, shallowEqual } from 'react-redux';
import type { RootState } from '../../store';
import { MessageBubble } from './MessageBubble';
import { ApprovalCard } from './ApprovalCard';
import { Composer } from './Composer';
import { TodoPanel } from './TodoPanel';
import { ToolCallItem } from './ToolTimeline';
import type { ToolCallEntry } from '../../store/chatRuntimeSlice';

export function ChatView() {
  const { dispatch } = useApp();
  const { t } = useT();
  const navigate = useNavigate();
  const { threadId: urlThreadId } = useParams<{ threadId?: string }>();
  const bottomRef = useRef<HTMLDivElement>(null);

  const { threads, activeThreadId, createNewThread, deleteSelectedThread, selectThread } = useThreads();
  const { pendingApprovals, decideApproval } = useApprovals();
  const { messages, streamingContent, streamingThinking, isStreaming, sendMessage } = useChatMessages();
  const toolCallsByMsg = useSelector((s: RootState) => s.chatRuntime.toolCallsByMsg, shallowEqual);
  const pendingToolCalls = useSelector((s: RootState) => s.chatRuntime.toolCalls[activeThreadId || ''] || [], shallowEqual);

  // Sync URL threadId to Redux when they differ (handles page reload, direct nav, bookmark).
  useEffect(() => {
    if (urlThreadId && urlThreadId !== activeThreadId) {
      selectThread(urlThreadId);
    }
  }, [urlThreadId, activeThreadId, selectThread]);

  const thread = threads.find(t => t.id === activeThreadId);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  const handleCreateThread = async () => {
    const t = await createNewThread();
    if (t) navigate(`/chat/${t.id}`);
  };

  return (
    <div className="flex-1 flex flex-col min-h-0">
      {/* Header */}
      <header className="flex items-center gap-4 px-6 py-3 border-b border-surface-border glass-surface shrink-0">
        <button className="btn-ghost !p-1.5" onClick={() => dispatch({ type: 'TOGGLE_SIDEBAR' })}>
          <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
          </svg>
        </button>
        <div className="flex-1">
          <h1 className="text-sm font-semibold text-white/90 truncate">
            {thread?.title || t('chat.newConversation')}
          </h1>
          {thread && (
            <p className="text-xs text-white/30">{t('chat.messages', { count: thread.message_count || 0 })}</p>
          )}
        </div>
        {thread && (
          <button className="btn-ghost text-xs" onClick={() => deleteSelectedThread(thread.id)}>
            {t('chat.delete')}
          </button>
        )}
      </header>

      {/* Empty state: no thread selected */}
      {!activeThreadId ? (
        <div className="flex-1 flex items-center justify-center">
          <div className="text-center space-y-4 animate-fade-in">
            <div className="w-16 h-16 mx-auto rounded-2xl bg-gradient-to-br from-ocean-400 to-ocean-600 flex items-center justify-center shadow-glow-ocean">
              <svg className="w-8 h-8 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
              </svg>
            </div>
            <h2 className="text-xl font-semibold text-white/80">{t('app.title')}</h2>
            <p className="text-white/40 max-w-md">{t('chat.selectThread')}</p>
            <button className="btn-primary" onClick={handleCreateThread}>
              {t('chat.newConversation')}
            </button>
          </div>
        </div>
      ) : (
        <>
          {/* Messages area */}
          <div className="flex-1 overflow-y-auto">
            <div className="max-w-3xl xl:max-w-5xl 2xl:max-w-6xl mx-auto px-6 py-6 space-y-6">
              {messages.length === 0 && !isStreaming ? (
                <div className="text-center text-white/30 py-12 animate-fade-in">
                  <p className="text-lg">{t('chat.startConversation')}</p>
                  <p className="text-sm mt-1">{t('chat.startHint')}</p>
                </div>
              ) : (
                <>
                  {messages.map((msg, i) => (
                    <MessageBubble key={msg.id || i} message={msg} toolCalls={toolCallsByMsg[msg.id]} />
                  ))}
                  {/* Pending tool calls: always visible, not just during streaming */}
                  {pendingToolCalls.length > 0 && (
                    <div className="mb-2">
                      <div className="text-xs text-ocean-400/70 mb-1.5 flex items-center gap-1.5">
                        <div className="w-3 h-3 rounded-full border-2 border-ocean-400 border-t-transparent animate-spin" />
                        Running {pendingToolCalls.length} tool{pendingToolCalls.length > 1 ? 's' : ''}...
                      </div>
                      {pendingToolCalls.map(call => (
                        <ToolCallItem key={call.id} call={call} />
                      ))}
                    </div>
                  )}
                  {isStreaming && (
                    <>
                      {streamingThinking.length > 0 && (
                        <details className="mb-2" open>
                          <summary className="text-xs text-white/40 cursor-pointer hover:text-white/60 transition-colors">
                            Thinking{streamingContent.length === 0 ? '...' : ''}
                          </summary>
                          <div className="mt-1 text-xs text-white/30 whitespace-pre-wrap font-mono leading-relaxed pl-3 border-l border-white/10">
                            {streamingThinking}
                          </div>
                        </details>
                      )}
                      {streamingContent.length > 0 && (
                        <MessageBubble
                          message={{ id: 0, role: 'assistant', content: streamingContent, created_at: new Date().toISOString() }}
                        />
                      )}
                    </>
                  )}
                  {pendingApprovals.map(approval => (
                    <ApprovalCard key={approval.id} approval={approval} onDecide={(id, d) => decideApproval(id, d)} />
                  ))}
                </>
              )}
              <div ref={bottomRef} />
            </div>
          </div>
          {/* Composer pinned at bottom */}
          <TodoPanel threadId={activeThreadId} />
          <Composer key={activeThreadId} onSend={sendMessage} disabled={isStreaming} />
        </>
      )}
    </div>
  );
}
