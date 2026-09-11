package memory

import "context"

// RPC provides Wails-bound memory pipeline methods.
//
// The pipeline and namespace manager are resolved through getters rather than
// captured at construction time: Wails evaluates the Bind list before
// OnStartup runs, so captured pointers would be nil for the whole process
// lifetime.
type MemoryRPC struct {
	pipelineFn func() *Pipeline
	nsMgrFn    func() *NamespaceManager
}

// NewMemoryRPC creates a memory RPC handler. Both getters are evaluated per call.
func NewMemoryRPC(pipelineFn func() *Pipeline, nsMgrFn func() *NamespaceManager) *MemoryRPC {
	return &MemoryRPC{pipelineFn: pipelineFn, nsMgrFn: nsMgrFn}
}

func (r *MemoryRPC) pipeline() *Pipeline {
	if r.pipelineFn == nil {
		return nil
	}
	return r.pipelineFn()
}

func (r *MemoryRPC) nsMgr() *NamespaceManager {
	if r.nsMgrFn == nil {
		return nil
	}
	return r.nsMgrFn()
}

// SearchMemory performs a multi-strategy memory search.
// filter: "all", "fts5", "vector", "graph", or "" (same as "all").
func (r *MemoryRPC) SearchMemory(query string, filter string) (string, error) {
	p := r.pipeline()
	if p == nil {
		return "Memory pipeline not available.", nil
	}
	result, err := p.SearchWithFilter(context.Background(), query, p.DefaultSearchLimit(), filter)
	if err != nil {
		return "", err
	}
	return result.Formatted(), nil
}

// ListNamespaces returns all distinct namespaces in memory.
func (r *MemoryRPC) ListNamespaces() ([]string, error) {
	m := r.nsMgr()
	if m == nil {
		return nil, nil
	}
	return m.List()
}

// ClearNamespace deletes all memory data for a namespace.
func (r *MemoryRPC) ClearNamespace(ns string) error {
	m := r.nsMgr()
	if m == nil {
		return nil
	}
	return m.Clear(ns)
}

// SetRetrievalProfile applies a named weight profile to the retriever.
func (r *MemoryRPC) SetRetrievalProfile(profile string) error {
	p := r.pipeline()
	if p == nil {
		return nil
	}
	p.ApplyRetrievalProfile(profile)
	return nil
}
