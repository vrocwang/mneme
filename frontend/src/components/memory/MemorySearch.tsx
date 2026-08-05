import { useState, useEffect, useCallback } from 'react';
import { useApp } from '../../state/AppContext';
import { useT } from '../../lib/i18n/I18nContext';
import * as api from '../../services/wails';

type FilterTab = 'all' | 'fts5' | 'vector' | 'graph';

interface MemoryStats {
  config: Record<string, unknown> | null;
  weights: Record<string, string> | null;
  checkpoints: unknown[];
}

export function MemorySearch() {
  const { dispatch } = useApp();
  const { t } = useT();
  const [query, setQuery] = useState('');
  const [result, setResult] = useState('');
  const [loading, setLoading] = useState(false);
  const [activeFilter, setActiveFilter] = useState<FilterTab>('all');
  const [hasSearched, setHasSearched] = useState(false);
  const [stats, setStats] = useState<MemoryStats>({ config: null, weights: null, checkpoints: [] });
  const [showStats, setShowStats] = useState(false);

  const loadStats = useCallback(async () => {
    const [cfg, w, cp] = await Promise.allSettled([
      api.getMemoryPipelineConfig(),
      api.getRetrievalWeights(),
      (async () => {
        try { return await api.getAppStateSnapshot(); } catch { return null; }
      })(),
    ]);
    setStats({
      config: cfg.status === 'fulfilled' ? cfg.value as unknown as Record<string, unknown> : null,
      weights: w.status === 'fulfilled' ? w.value as unknown as Record<string, string> : null,
      checkpoints: [],
    });
  }, []);

  useEffect(() => { loadStats(); }, [loadStats]);

  const filters: { key: FilterTab; label: string; desc: string }[] = [
    { key: 'all', label: t('memory.filterAll'), desc: t('memory.filterAllDesc') },
    { key: 'fts5', label: t('memory.filterFTS5'), desc: t('memory.filterFTS5Desc') },
    { key: 'vector', label: t('memory.filterVector'), desc: t('memory.filterVectorDesc') },
    { key: 'graph', label: t('memory.filterGraph'), desc: t('memory.filterGraphDesc') },
  ];

  async function search() {
    if (!query.trim()) return;
    setLoading(true);
    setHasSearched(true);
    try {
      const r = await api.searchMemory(query.trim(), activeFilter);
      setResult(r || '');
    } catch {
      setResult('');
    } finally {
      setLoading(false);
    }
  }

  // Parse result sections (the backend returns formatted text with headers).
  const sections = result ? parseMemoryResult(result) : [];

  return (
    <div className="flex-1 flex flex-col min-h-0">
      <header className="flex items-center gap-4 px-6 py-3 border-b border-surface-border glass-surface shrink-0">
        <button className="btn-ghost !p-1.5" onClick={() => dispatch({ type: 'TOGGLE_SIDEBAR' })}>
          <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
          </svg>
        </button>
        <h1 className="text-sm font-semibold text-white/90">{t('memory.title')}</h1>
        <button
          onClick={() => setShowStats(!showStats)}
          className={`ml-auto text-xs px-2 py-1 rounded transition-colors ${showStats ? 'bg-ocean-500/20 text-ocean-400' : 'text-white/30 hover:text-white/60'}`}
        >
          {t('diagnostics.title')}
        </button>
      </header>

      <div className="flex-1 overflow-y-auto p-6">
        <div className="max-w-3xl xl:max-w-4xl 2xl:max-w-5xl mx-auto space-y-6 animate-fade-in">
          {/* Stats panel */}
          {showStats && (
            <div className="card space-y-3 animate-fade-in">
              <h3 className="text-sm font-semibold text-white/70">{t('memory.config')}</h3>
              {stats.config && (
                <div className="grid grid-cols-2 gap-2 text-xs">
                  {Object.entries(stats.config).map(([k, v]) => (
                    <div key={k} className="flex justify-between bg-surface-muted rounded px-2 py-1">
                      <span className="text-white/40">{k}</span>
                      <span className="text-white/70 font-mono">{String(v)}</span>
                    </div>
                  ))}
                </div>
              )}
              {stats.weights && (
                <div className="pt-2 border-t border-surface-border">
                  <h4 className="text-xs font-semibold text-white/50 mb-2">{t('memory.weights')}</h4>
                  <div className="flex items-center gap-2 mb-3">
                    <span className="text-xs text-white/30">{t('memory.profile') || 'Profile'}:</span>
                    <select
                      className="input-field !py-1 !text-xs"
                      value={stats.weights.profile || 'balanced'}
                      onChange={async e => {
                        const profile = e.target.value;
                        try {
                          await api.setRetrievalProfile(profile);
                          await loadStats();
                        } catch { /* browser mode — ignore */ }
                      }}
                    >
                      <option value="balanced">Balanced</option>
                      <option value="semantic">Semantic</option>
                      <option value="lexical">Lexical</option>
                      <option value="graph_first">Graph First</option>
                    </select>
                  </div>
                  <div className="grid grid-cols-2 gap-2 text-xs">
                    {Object.entries(stats.weights).filter(([k]) => k !== 'profile').map(([k, v]) => (
                      <div key={k} className="flex justify-between bg-surface-muted rounded px-2 py-1">
                        <span className="text-white/40">{k}</span>
                        <span className="text-white/70 font-mono">{String(v)}</span>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          )}

          {/* Search bar */}
          <div className="flex gap-3">
            <input
              className="input-field flex-1"
              placeholder={t('memory.searchPlaceholder')}
              value={query}
              onChange={e => setQuery(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && search()}
            />
            <button className="btn-primary" disabled={loading || !query.trim()} onClick={search}>
              {loading ? t('memory.searching') : t('memory.search')}
            </button>
          </div>

          {/* Filter tabs */}
          <div className="flex gap-1 p-1 bg-surface-overlay rounded-lg">
            {filters.map(f => (
              <button
                key={f.key}
                onClick={() => setActiveFilter(f.key)}
                className={`flex-1 px-3 py-1.5 text-xs rounded-md transition-colors ${
                  activeFilter === f.key
                    ? 'bg-ocean-500/20 text-ocean-400'
                    : 'text-white/40 hover:text-white/60'
                }`}
                title={f.desc}
              >
                {f.label}
              </button>
            ))}
          </div>

          {/* Results */}
          {loading && (
            <div className="space-y-3">
              {[1, 2, 3].map(i => (
                <div key={i} className="h-16 bg-surface-overlay rounded-xl animate-pulse" />
              ))}
            </div>
          )}

          {!loading && hasSearched && (
            <div className="space-y-2">
              <p className="text-xs text-white/30">
                {t('memory.resultsFor')} "{query}"
              </p>
              {sections.length > 0 ? (
                <div className="space-y-3">
                  {sections.map((section, i) => (
                    <div key={i} className="card p-4">
                      {section.header && (
                        <h4 className="text-xs font-semibold text-ocean-400 mb-2 uppercase tracking-wider">
                          {section.header}
                        </h4>
                      )}
                      <div className="text-sm text-white/70 whitespace-pre-wrap leading-relaxed font-mono max-h-40 overflow-y-auto">
                        {section.body}
                      </div>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="text-center text-white/25 py-16">
                  <p>{t('memory.noResults')}</p>
                </div>
              )}
            </div>
          )}

          {!result && !loading && !hasSearched && (
            <div className="text-center text-white/25 py-16">
              <svg className="w-12 h-12 mx-auto mb-4 opacity-30" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
              </svg>
              <p>{t('memory.emptyHint')}</p>
              <p className="text-xs mt-1">{t('memory.emptySub')}</p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// parseMemoryResult splits the formatted memory search output into sections.
// The backend returns text with "─── Section Name ───" headers separating groups.
function parseMemoryResult(text: string): { header?: string; body: string }[] {
  const sections: { header?: string; body: string }[] = [];
  const lines = text.split('\n');
  let currentHeader: string | undefined;
  let currentBody: string[] = [];

  for (const line of lines) {
    const headerMatch = line.match(/^[─═]+(.+?)[─═]+$/);
    if (headerMatch) {
      if (currentBody.length > 0 || currentHeader) {
        sections.push({ header: currentHeader, body: currentBody.join('\n').trim() });
      }
      currentHeader = headerMatch[1].trim();
      currentBody = [];
    } else {
      currentBody.push(line);
    }
  }
  if (currentBody.length > 0 || currentHeader) {
    sections.push({ header: currentHeader, body: currentBody.join('\n').trim() });
  }

  if (sections.length === 0 && text.trim()) {
    return [{ body: text.trim() }];
  }
  return sections;
}
