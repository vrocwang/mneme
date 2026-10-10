import { useState, useEffect, useCallback } from 'react';
import * as api from '../../services/wails';
import type { TodoSnapshot } from '../../services/wails';
import { useApp } from '../../state/AppContext';
import { useT } from '../../lib/i18n/I18nContext';

export function TodoPanel({ threadId }: { threadId: string }) {
  const { t } = useT();
  const { actions } = useApp();
  const [todos, setTodos] = useState<TodoSnapshot | null>(null);
  const [collapsed, setCollapsed] = useState(true);
  const [newTitle, setNewTitle] = useState('');

  useEffect(() => {
    if (collapsed) return;
    let cancelled = false;
    api.listTodos(threadId)
      .then(snapshot => { if (!cancelled) setTodos(snapshot); })
      .catch(e => { if (!cancelled) actions.addToast('error', t('errors.loadTasks'), String(e)); });
    return () => { cancelled = true; };
  }, [threadId, collapsed, actions, t]);

  // Every todo mutation returns the new snapshot; on failure the board keeps
  // its last known state and the error is surfaced.
  const apply = useCallback(async (op: () => Promise<TodoSnapshot>, failTitle: string) => {
    try {
      setTodos(await op());
      return true;
    } catch (e) {
      actions.addToast('error', failTitle, String(e));
      return false;
    }
  }, [actions]);

  async function add(title: string) {
    if (!title.trim()) return;
    if (await apply(() => api.addTodo(threadId, title.trim(), ''), t('errors.addTask'))) {
      setNewTitle('');
    }
  }

  async function toggle(cardId: string, currentStatus: string) {
    const next = currentStatus === 'done' ? 'todo' : 'done';
    await apply(() => api.updateTodoStatus(threadId, cardId, next), t('errors.updateTask'));
  }

  return (
    <div className="shrink-0 border-t border-surface-border">
      <button
        className="w-full flex items-center gap-2 px-6 py-2 text-xs text-white/40 hover:text-white/60 transition-colors"
        onClick={() => setCollapsed(!collapsed)}
      >
        <svg className={`w-3.5 h-3.5 transition-transform ${collapsed ? '' : 'rotate-90'}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
        </svg>
        {t('todo.title')} {todos && `(${todos.cards.length})`}
      </button>
      {!collapsed && (
        <div className="px-6 pb-3 space-y-2 animate-slide-up">
          <div className="flex gap-2">
            <input
              className="input-field !py-1 !text-xs"
              placeholder={t('todo.addPlaceholder')}
              value={newTitle}
              onChange={e => setNewTitle(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && add(newTitle)}
            />
            <button className="btn-primary !py-1 !px-3 !text-xs" onClick={() => add(newTitle)}>{t('todo.add')}</button>
          </div>
          <div className="space-y-1 max-h-32 overflow-y-auto">
            {todos?.cards.map(card => (
              <div key={card.id} className="flex items-center gap-2 text-xs group">
                <input
                  type="checkbox"
                  checked={card.status === 'done'}
                  onChange={() => toggle(card.id, card.status)}
                  className="rounded border-surface-border bg-surface"
                />
                <span className={`flex-1 min-w-0 truncate ${card.status === 'done' ? 'line-through text-white/20' : 'text-white/60'}`}>
                  {card.title}
                </span>
                <button
                  className="opacity-0 group-hover:opacity-100 text-coral-400/60 hover:text-coral-400"
                  onClick={() => apply(() => api.removeTodo(threadId, card.id), t('errors.removeTask'))}
                >
                  <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                  </svg>
                </button>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
