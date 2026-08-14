package memory

import "context"

// RPC provides Wails-bound memory pipeline methods.
type MemoryRPC struct {
	pipeline *Pipeline
	nsMgr    *NamespaceManager
}

// NewRPC creates a memory RPC handler.
func NewMemoryRPC(pipeline *Pipeline, nsMgr *NamespaceManager) *MemoryRPC {
	return &MemoryRPC{pipeline: pipeline, nsMgr: nsMgr}
}

// SearchMemory performs a multi-strategy memory search.
// filter: "all", "fts5", "vector", "graph", or "" (same as "all").
func (r *MemoryRPC) SearchMemory(query string, filter string) (string, error) {
	if r.pipeline == nil {
		return "Memory pipeline not available.", nil
	}
	result, err := r.pipeline.SearchWithFilter(context.Background(), query, r.pipeline.DefaultSearchLimit(), filter)
	if err != nil {
		return "", err
	}
	return result.Formatted(), nil
}

// ListNamespaces returns all distinct namespaces in memory.
func (r *MemoryRPC) ListNamespaces() ([]string, error) {
	if r.nsMgr == nil {
		return nil, nil
	}
	return r.nsMgr.List()
}

// ClearNamespace deletes all memory data for a namespace.
func (r *MemoryRPC) ClearNamespace(ns string) error {
	if r.nsMgr == nil {
		return nil
	}
	return r.nsMgr.Clear(ns)
}

// SetRetrievalProfile applies a named weight profile to the retriever.
func (r *MemoryRPC) SetRetrievalProfile(profile string) error {
	if r.pipeline == nil {
		return nil
	}
	r.pipeline.ApplyRetrievalProfile(profile)
	return nil
}
