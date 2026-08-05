package sync

import (
	"context"
	"fmt"
	"time"
)

// MCPPipeline syncs data from an MCP server into the memory store.
// It acts as a TickPipeline adapter over any MCP server that exposes
// tools for listing/fetching content.
type MCPPipeline struct {
	name     string
	serverID string
	interval time.Duration
	lastSync time.Time
	// fetchFunc is called on each sync tick. Implementations would call
	// into the MCP client to list resources and pull their content.
	fetchFunc func(ctx context.Context) ([]Item, error)
}

// NewMCPPipeline creates an MCP-backed sync pipeline.
func NewMCPPipeline(serverID string, fetchFunc func(ctx context.Context) ([]Item, error), interval time.Duration) *MCPPipeline {
	if interval == 0 {
		interval = 15 * time.Minute
	}
	return &MCPPipeline{
		name:      "mcp:" + serverID,
		serverID:  serverID,
		interval:  interval,
		fetchFunc: fetchFunc,
	}
}

func (p *MCPPipeline) Name() string            { return p.name }
func (p *MCPPipeline) Kind() PipelineKind      { return KindMcp }
func (p *MCPPipeline) Interval() time.Duration { return p.interval }
func (p *MCPPipeline) LastSync() time.Time     { return p.lastSync }

func (p *MCPPipeline) Sync(ctx context.Context) ([]Item, error) {
	if p.fetchFunc == nil {
		return nil, fmt.Errorf("mcp pipeline %s: no fetch function configured", p.serverID)
	}
	items, err := p.fetchFunc(ctx)
	p.lastSync = time.Now()
	return items, err
}
