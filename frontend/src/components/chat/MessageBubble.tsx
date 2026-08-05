import { useState, useCallback, useMemo } from 'react';
import ReactMarkdown from 'react-markdown';
import type { MessageRecord } from '../../services/wails';
import { useT } from '../../lib/i18n/I18nContext';
import { ThinkingBlock } from './ThinkingBlock';
import { ToolCallItem } from './ToolTimeline';
import type { ToolCallEntry } from '../../store/chatRuntimeSlice';

const proseClasses = `prose prose-sm prose-invert max-w-none
  prose-pre:bg-black/40 prose-pre:border prose-pre:border-surface-border
  prose-code:bg-ocean-500/20 prose-code:px-1 prose-code:py-0.5 prose-code:rounded prose-code:text-xs
  prose-headings:text-white/90 prose-p:leading-relaxed
  prose-a:text-ocean-400 prose-a:underline
  prose-strong:text-white/90
  prose-ul:my-1 prose-ol:my-1
  prose-li:my-0.5
  [&_pre]:overflow-x-auto [&_pre]:rounded-lg
  [&_code]:text-xs`;

interface ContentSegment {
  kind: 'text' | 'math_inline' | 'math_block' | 'thinking';
  content: string;
}

function parseContent(raw: string): ContentSegment[] {
  const segments: ContentSegment[] = [];
  let remaining = raw;

  while (remaining.length > 0) {
    // Check for thinking blocks: <thinking>...</thinking>
    const thinkMatch = remaining.match(/<thinking>([\s\S]*?)<\/thinking>/);
    // Check for display math: $$...$$
    const blockMathMatch = remaining.match(/\$\$([\s\S]*?)\$\$/);
    // Check for inline math: $...$ (but not $$), requires at least one non-digit char
    // to avoid matching currency like $5
    const inlineMathMatch = remaining.match(/(?<!\$)\$(?!\$)([^$\n]*[^$\d\n][^$\n]*)\$(?!\$)/);

    const matches = [
      { re: thinkMatch, kind: 'thinking' as const, idx: thinkMatch?.index ?? Infinity, len: thinkMatch?.[0].length ?? 0 },
      { re: blockMathMatch, kind: 'math_block' as const, idx: blockMathMatch?.index ?? Infinity, len: blockMathMatch?.[0].length ?? 0 },
      { re: inlineMathMatch, kind: 'math_inline' as const, idx: inlineMathMatch?.index ?? Infinity, len: inlineMathMatch?.[0].length ?? 0 },
    ];

    matches.sort((a, b) => a.idx - b.idx);
    const first = matches[0];

    if (first.idx === Infinity || first.idx < 0) {
      segments.push({ kind: 'text', content: remaining });
      break;
    }

    if (first.idx > 0) {
      segments.push({ kind: 'text', content: remaining.slice(0, first.idx) });
    }

    segments.push({ kind: first.kind, content: first.re![1] });
    remaining = remaining.slice(first.idx + first.len);
  }

  return segments;
}

function MathDisplay({ content, block }: { content: string; block?: boolean }) {
  const Tag = block ? 'div' : 'span';
  return (
    <Tag
      className={`font-mono text-ocean-300/90 ${block ? 'block my-2 py-2 px-3 rounded-lg bg-ocean-500/10 border border-ocean-500/20 overflow-x-auto text-center' : 'px-0.5'}`}
    >
      {content.trim()}
    </Tag>
  );
}

export function MessageBubble({ message, isStreaming, toolCalls }: { message: MessageRecord; isStreaming?: boolean; toolCalls?: ToolCallEntry[] }) {
  const { t } = useT();
  const isUser = message.role === 'user';
  const isTool = message.role === 'tool';
  const showMarkdown = !isUser && !isTool;
  const [copied, setCopied] = useState(false);

  const copyContent = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(message.content);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch { /* clipboard API may not be available */ }
  }, [message.content]);

  const segments = useMemo(() => parseContent(message.content), [message.content]);
  const hasSpecial = segments.some(s => s.kind !== 'text');

  return (
    <div className={`flex gap-3 animate-slide-up ${isUser ? 'justify-end' : 'justify-start'}`}>
      {!isUser && (
        <div className="w-7 h-7 rounded-full bg-gradient-to-br from-ocean-400 to-ocean-600 flex items-center justify-center shrink-0 mt-0.5">
          <svg className="w-3.5 h-3.5 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
          </svg>
        </div>
      )}
      <div className={`max-w-[75%] ${isUser ? 'order-1' : ''}`}>
        <div
          className={`px-4 py-3 rounded-2xl text-sm leading-relaxed ${
            isUser
              ? 'bg-ocean-500/20 border border-ocean-500/30 rounded-br-md'
              : isTool
                ? 'bg-amber-500/10 border border-amber-500/20 rounded-bl-md font-mono text-xs'
                : 'bg-surface-overlay border border-surface-border rounded-bl-md'
          }`}
        >
          {isTool && (
            <div className="flex items-center gap-2 mb-1 text-amber-400/70 text-xs">
              <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.066 2.573c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.573 1.066c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.066-2.573c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
              </svg>
              {t('chat.toolResult')}
            </div>
          )}
          {showMarkdown ? (
            hasSpecial ? (
              segments.map((seg, i) => {
                if (seg.kind === 'thinking') return <ThinkingBlock key={i} content={seg.content} />;
                if (seg.kind === 'math_inline') return <MathDisplay key={i} content={seg.content} />;
                if (seg.kind === 'math_block') return <MathDisplay key={i} content={seg.content} block />;
                return <div key={i} className={proseClasses}><ReactMarkdown>{seg.content}</ReactMarkdown></div>;
              })
            ) : (
              <div className={proseClasses}>
                <ReactMarkdown>{message.content}</ReactMarkdown>
              </div>
            )
          ) : (
            <div className={`${isStreaming ? 'after:content-["▊"] after:animate-pulse after:text-ocean-400' : ''} whitespace-pre-wrap break-words [overflow-wrap:anywhere]`}>
              {message.content}
            </div>
          )}
        </div>
        {toolCalls && toolCalls.length > 0 && (
          <div className="mt-2 space-y-1.5">
            {toolCalls.map(call => (
              <ToolCallItem key={call.id} call={call} />
            ))}
          </div>
        )}
        <div className={`flex items-center gap-2 mt-1 ${isUser ? 'justify-end' : ''}`}>
          <span className="text-xs text-white/20">
            {message.role === 'user' ? t('chat.you') : isTool ? t('chat.tool') : t('chat.assistant')}
          </span>
          {message.content && (
            <button
              onClick={copyContent}
              className="text-xs text-white/10 hover:text-white/40 transition-colors"
              title={t('chat.copy')}
            >
              {copied ? t('chat.copied') : t('chat.copy')}
            </button>
          )}
        </div>
      </div>
      {isUser && (
        <div className="w-7 h-7 rounded-full bg-sage-500/30 flex items-center justify-center shrink-0 mt-0.5">
          <svg className="w-3.5 h-3.5 text-sage-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
          </svg>
        </div>
      )}
    </div>
  );
}
