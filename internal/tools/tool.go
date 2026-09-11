package tools

import (
	"context"

	pkgtools "github.com/simon/mneme/pkg/tools"
)

// Re-export shared types from pkg/tools so existing internal/ code continues
// to work unchanged. Extensions can import pkg/tools directly.
type (
	PermissionLevel = pkgtools.PermissionLevel
	ToolCategory    = pkgtools.ToolCategory
	ToolSource      = pkgtools.ToolSource
	Transport       = pkgtools.Transport
	ToolHealth      = pkgtools.ToolHealth
	ToolScope       = pkgtools.ToolScope
	Schema          = pkgtools.Schema
	Result          = pkgtools.Result
	Tool            = pkgtools.Tool
	DescribedTool   = pkgtools.DescribedTool
	ToolDescriptor  = pkgtools.ToolDescriptor
)

// Re-export constants.
const (
	PermNone      = pkgtools.PermNone
	PermReadOnly  = pkgtools.PermReadOnly
	PermWrite     = pkgtools.PermWrite
	PermExecute   = pkgtools.PermExecute
	PermDangerous = pkgtools.PermDangerous

	CategorySystem      = pkgtools.CategorySystem
	CategorySkill       = pkgtools.CategorySkill
	CategoryInteraction = pkgtools.CategoryInteraction

	SourceCore      = pkgtools.SourceCore
	SourceExtension = pkgtools.SourceExtension
	SourceMCP       = pkgtools.SourceMCP
	SourceCustom    = pkgtools.SourceCustom

	TransportInProcess = pkgtools.TransportInProcess
	TransportJsonRpc   = pkgtools.TransportJsonRpc
	TransportMcpStdio  = pkgtools.TransportMcpStdio

	HealthAvailable = pkgtools.HealthAvailable
	HealthUnknown   = pkgtools.HealthUnknown
	HealthDegraded  = pkgtools.HealthDegraded
	HealthUnhealthy = pkgtools.HealthUnhealthy

	ScopeAll       = pkgtools.ScopeAll
	ScopeAgentOnly = pkgtools.ScopeAgentOnly
	ScopeCliOnly   = pkgtools.ScopeCliOnly
)

// Re-export functions.
var (
	ParsePermissionLevel = pkgtools.ParsePermissionLevel
)

// Re-export the shared agent-loop default.
const DefaultMaxIterations = pkgtools.DefaultMaxIterations

// ToolCallOptions carries per-invocation hints from the agent harness.
type ToolCallOptions struct {
	PreferMarkdown  bool
	TimeoutOverride int
	CallID          string
}

// BaseTool is a convenience embed for tools that only need Schema + Execute.
type BaseTool struct {
	SchemaVal         Schema
	PermLevel         PermissionLevel
	HasSideEffects    bool
	IsConcurrencySafe bool
	MaxOutputChars    int
	ToolCategory      ToolCategory
}

func (b *BaseTool) Schema() Schema                   { return b.SchemaVal }
func (b *BaseTool) PermissionLevel() PermissionLevel { return b.PermLevel }
func (b *BaseTool) SideEffects() bool                { return b.HasSideEffects }
func (b *BaseTool) ConcurrencySafe() bool            { return b.IsConcurrencySafe }
func (b *BaseTool) MaxResultChars() int              { return b.MaxOutputChars }
func (b *BaseTool) Category() ToolCategory           { return b.ToolCategory }

// Ensure BaseTool satisfies common interfaces.
var (
	_ interface{ PermissionLevel() PermissionLevel } = (*BaseTool)(nil)
	_ interface{ SideEffects() bool }                = (*BaseTool)(nil)
	_ interface{ ConcurrencySafe() bool }            = (*BaseTool)(nil)
	_ interface{ MaxResultChars() int }              = (*BaseTool)(nil)
	_ interface{ Category() ToolCategory }           = (*BaseTool)(nil)
)

// ── Optional capability interfaces (internal-only) ──────────────────────
//
// Only interfaces that are actually type-asserted somewhere are declared here.
// A declared-but-never-asserted interface is dead weight: extensions are
// separate processes and cannot implement an internal/ interface, so it could
// never gain an implementation.

// PermissionedTool allows a tool to declare its default risk tier.
type PermissionedTool interface {
	Tool
	PermissionLevel() PermissionLevel
}

// SideEffectTool reports whether a tool modifies state outside the agent.
type SideEffectTool interface {
	Tool
	SideEffects() bool
}

// CategorizedTool allows a tool to declare its category.
type CategorizedTool interface {
	Tool
	Category() ToolCategory
}

// ConcurrencySafeTool reports whether a tool can run concurrently.
type ConcurrencySafeTool interface {
	Tool
	ConcurrencySafe() bool
}

// ConcurrencySafeWithArgs reports concurrency safety based on arguments.
type ConcurrencySafeWithArgs interface {
	Tool
	ConcurrencySafeWithArgs(args map[string]interface{}) bool
}

// Ensure Tool still referenced (for tools that use context).
var _ context.Context
