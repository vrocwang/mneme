package approval

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/simon/mneme/internal/agent"
)

// ApprovalEventPublisher is an interface for publishing approval lifecycle
// events. Implementations should be non-blocking (publish via a buffered
// channel or goroutine) to avoid stalling the approval gate.
type ApprovalEventPublisher interface {
	PublishApprovalEvent(kind string, data map[string]interface{})
}

// Gate is the async approval middleware. It parks tool calls that need user consent,
// persists pending requests to SQLite so they survive restarts, and records audit entries.
//
// The gate supports origin-aware policies: automation turns (cron, subconscious) can
// auto-approve tools up to a configured risk level; external channel messages require
// approval for everything; web chat and CLI fall in between.
type Gate struct {
	store   *Store
	events  ApprovalEventPublisher
	log     *slog.Logger
	timeout time.Duration
	enabled bool

	mu           sync.Mutex
	pending      map[string]*PendingApproval // in-memory map keyed by approval ID
	originPolicy OriginPolicyConfig          // per-origin approval rules
	resolveTool  ToolResolver                // registry-backed (nil = heuristic fallback)
}

// OriginPolicyConfig specifies per-origin approval behaviour.
type OriginPolicyConfig struct {
	// AutoApproveRisk maps turn origin kinds to the highest tool permission level
	// that can be auto-approved without prompting. Tools at or below this level skip
	// the gate entirely. absent or "none" means no auto-approval.
	AutoApproveRisk map[agent.TurnOriginKind]string `json:"auto_approve_risk"`
	// RequireAll forces every tool through the gate regardless of risk, per origin.
	RequireAll map[agent.TurnOriginKind]bool `json:"require_all"`
}

// SetEventPublisher replaces the event publisher after construction.
// Used when the event bus is created after the gate (e.g. AppCore.Init flow).
func (g *Gate) SetEventPublisher(events ApprovalEventPublisher) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.events = events
}

// NewGate creates an approval gate backed by the given store.
func NewGate(store *Store, events ApprovalEventPublisher, log *slog.Logger, enabled bool) *Gate {
	if log == nil {
		log = slog.Default()
	}
	g := &Gate{
		store:   store,
		events:  events,
		log:     log,
		timeout: 10 * time.Minute,
		enabled: enabled,
		pending: make(map[string]*PendingApproval),
	}
	// Set sane origin-aware defaults.
	g.SetOriginPolicy(DefaultOriginPolicy())
	// Clean up stale entries from previous session.
	go g.recoverStale()
	return g
}

// DefaultOriginPolicy returns sensible per-origin approval rules.
func DefaultOriginPolicy() OriginPolicyConfig {
	return OriginPolicyConfig{
		AutoApproveRisk: map[agent.TurnOriginKind]string{
			agent.OriginWebChat:           "read_only",
			agent.OriginCLI:               "read_only",
			agent.OriginTrustedAutomation: "write",
			// OriginExternalChannel and OriginUnknown: absent = no auto-approval
		},
		RequireAll: map[agent.TurnOriginKind]bool{
			agent.OriginExternalChannel: true,
			agent.OriginUnknown:         true,
		},
	}
}

// SetOriginPolicy replaces the origin-aware approval policy at runtime.
func (g *Gate) SetOriginPolicy(policy OriginPolicyConfig) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.originPolicy = policy
}

// SetToolResolver installs a registry-backed resolver for tool risk/external-effect
// lookups. When set, it replaces the hardcoded heuristics so new tools added at
// runtime are correctly classified.
func (g *Gate) SetToolResolver(r ToolResolver) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.resolveTool = r
}

// SetEnabled toggles the gate at runtime.
func (g *Gate) SetEnabled(enabled bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.enabled = enabled
}

// IsEnabled returns whether the gate is active.
func (g *Gate) IsEnabled() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.enabled
}

// IsAllowlisted checks whether a tool has been permanently allowed.
func (g *Gate) IsAllowlisted(toolName string) bool {
	if g.store == nil {
		return false
	}
	return g.store.IsAllowlisted(toolName)
}

// RequestApproval parks a tool call and waits for a user decision.
// Returns the decision (or a timeout denial) and an audit entry.
//
// The method consults origin-aware policy before deciding whether to park:
//   - If the tool is allowlisted → auto-approve.
//   - If origin policy says "require all" → park.
//   - If origin policy specifies a risk level and the tool's risk is below it → auto-approve.
func (g *Gate) RequestApproval(ctx context.Context, toolName string, argsJSON string, reason string) (Decision, *AuditEntry) {
	if !g.enabled {
		return DecisionApproveOnce, nil
	}

	// If the tool is allowlisted, skip the prompt.
	if g.IsAllowlisted(toolName) {
		g.log.Debug("approval skipped (allowlisted)", "tool", toolName)
		return DecisionApproveOnce, nil
	}

	// ── Origin-aware auto-approval ────────────────────────────────────
	origin := agent.TurnOriginFromCtx(ctx)
	originStr := string(origin.Kind)

	g.mu.Lock()
	policy := g.originPolicy
	g.mu.Unlock()

	// SubconsciousTainted: subconscious tick with externally-synced memory content.
	// Deny any tool with external effects — the tainted memory could instruct
	// harmful actions without a user in the loop.
	if origin.IsSubconsciousTainted() && g.toolHasSideEffects(toolName) {
		g.log.Warn("approval denied (subconscious_tainted external-effect tool)",
			"tool", toolName, "origin", originStr, "automation_source", origin.AutomationSource)
		// Create a brief PendingApproval so the frontend poll sees the
		// denial instead of silently ignoring the tool call. The ApprovalCard
		// will display the denial reason and auto-expire after 30 seconds.
		now := time.Now().UTC()
		denyID := newID()
		denied := &PendingApproval{
			ID:        denyID,
			ToolName:  toolName,
			Args:      RedactArgsJSON(argsJSON),
			Reason:    "subconscious_tainted external-effect tool denied: " + reason,
			Origin:    originStr,
			CreatedAt: now,
			ExpiresAt: now.Add(30 * time.Second),
			resultCh:  make(chan Decision, 1),
		}
		g.mu.Lock()
		g.pending[denyID] = denied
		g.mu.Unlock()

		return DecisionDeny, &AuditEntry{
			ID:        newID(),
			ToolName:  toolName,
			Args:      RedactArgsJSON(argsJSON),
			Decision:  "denied",
			Reason:    "subconscious_tainted external-effect tool denied",
			Origin:    originStr,
			CreatedAt: time.Now().UTC(),
			DecidedAt: time.Now().UTC(),
		}
	}

	// RequireAll: force every tool through the gate for this origin.
	if policy.RequireAll != nil && policy.RequireAll[origin.Kind] {
		// Fall through to parking.
	} else if riskLevel, ok := policy.AutoApproveRisk[origin.Kind]; ok {
		// Check if the tool's risk level is within the auto-approve threshold.
		if g.toolRiskAtOrBelow(toolName, riskLevel) {
			g.log.Debug("approval auto-approved by origin policy",
				"tool", toolName, "origin", originStr, "risk", riskLevel)
			return DecisionApproveOnce, &AuditEntry{
				ID:        newID(),
				ToolName:  toolName,
				Args:      RedactArgsJSON(argsJSON),
				Decision:  "auto_approved",
				Reason:    "origin=" + originStr + " risk≤" + riskLevel,
				Origin:    originStr,
				CreatedAt: time.Now().UTC(),
				DecidedAt: time.Now().UTC(),
			}
		}
	}

	id := newID()
	now := time.Now().UTC()
	expires := now.Add(g.timeout)

	// Redact args before persistence/broadcast; raw args stay in memory
	// for the actual tool execution.
	redactedArgs := RedactArgsJSON(argsJSON)

	req := &PendingApproval{
		ID:        id,
		ToolName:  toolName,
		Args:      argsJSON, // raw — for execution
		Reason:    reason,
		Origin:    originStr,
		CreatedAt: now,
		ExpiresAt: expires,
		resultCh:  make(chan Decision, 1),
	}

	// Persist to SQLite so the request survives a restart.
	// Use REDACTED args for persistence.
	if g.store != nil {
		redactedReq := *req
		redactedReq.Args = redactedArgs
		if err := g.store.SavePending(&redactedReq); err != nil {
			g.log.Warn("failed to persist pending approval", "id", id, "error", err)
		}
	}

	g.mu.Lock()
	g.pending[id] = req
	g.mu.Unlock()

	g.log.Info("approval requested", "id", id, "tool", toolName)

	// Publish domain event so subscribers (notifications, audit, bus) can react.
	// Use REDACTED args for the event payload.
	if g.events != nil {
		g.events.PublishApprovalEvent("requested", map[string]interface{}{
			"id":     id,
			"tool":   toolName,
			"args":   redactedArgs,
			"reason": reason,
			"origin": originStr,
		})
	}

	// Park until decision, timeout, or context cancellation.
	var decision Decision
	select {
	case decision = <-req.resultCh:
		g.log.Debug("approval decided", "id", id, "decision", decision)
	case <-time.After(g.timeout):
		// Re-check the store before denying: a user may have approved the
		// request right at the TTL boundary, and the channel send raced with
		// the timer. If the store shows an approve decision, use it.
		if g.store != nil {
			if stored, err := g.store.GetDecision(id); err == nil && stored != nil {
				decision = *stored
				g.log.Info("approval resolved from store (TTL race)", "id", id, "decision", decision)
				break
			}
		}
		g.log.Info("approval timed out", "id", id, "tool", toolName)
		decision = DecisionDeny
	case <-ctx.Done():
		g.log.Info("approval cancelled", "id", id, "tool", toolName, "reason", ctx.Err())
		decision = DecisionDeny
	}

	// Cleanup.
	g.mu.Lock()
	delete(g.pending, id)
	g.mu.Unlock()

	if g.store != nil {
		g.store.DeletePending(id)
	}

	entry := &AuditEntry{
		ID:        id,
		ToolName:  toolName,
		Args:      redactedArgs, // persisted with PII scrubbed
		Decision:  decision.String(),
		Reason:    reason,
		Origin:    originStr,
		CreatedAt: now,
		DecidedAt: time.Now().UTC(),
	}

	// Persist always-allow decisions.
	if decision == DecisionApproveAlways && g.store != nil {
		if err := g.store.AddToAllowlist(toolName); err != nil {
			g.log.Warn("failed to add to allowlist", "tool", toolName, "error", err)
		}
	}

	// Record audit entry.
	if g.store != nil {
		if err := g.store.RecordAudit(*entry); err != nil {
			g.log.Warn("failed to record audit entry", "id", id, "error", err)
		}
	}

	// Publish decision event for subscribers (notifications, audit, bus).
	// Use REDACTED args for the event payload.
	if g.events != nil {
		g.events.PublishApprovalEvent("decided", map[string]interface{}{
			"id":       id,
			"tool":     toolName,
			"args":     redactedArgs,
			"decision": decision.String(),
			"origin":   originStr,
		})
	}

	return decision, entry
}

// Decide resolves a pending approval by ID.
func (g *Gate) Decide(id string, decision Decision) error {
	g.mu.Lock()
	req, ok := g.pending[id]
	g.mu.Unlock()

	if !ok || req == nil {
		return fmt.Errorf("approval %q not found (may have timed out)", id)
	}

	select {
	case req.resultCh <- decision:
		return nil
	default:
		return fmt.Errorf("approval %q already decided", id)
	}
}

// ListPending returns all currently pending approval requests (for UI display).
func (g *Gate) ListPending() []PendingApproval {
	g.mu.Lock()
	defer g.mu.Unlock()

	out := make([]PendingApproval, 0, len(g.pending))
	for _, req := range g.pending {
		out = append(out, PendingApproval{
			ID:        req.ID,
			ToolName:  req.ToolName,
			Args:      req.Args,
			Reason:    req.Reason,
			CreatedAt: req.CreatedAt,
			ExpiresAt: req.ExpiresAt,
		})
	}
	return out
}

// CancelAll denies all pending requests (called on shutdown).
func (g *Gate) CancelAll(reason string) {
	g.mu.Lock()
	requests := make([]*PendingApproval, 0, len(g.pending))
	for id, req := range g.pending {
		requests = append(requests, req)
		delete(g.pending, id)
	}
	g.mu.Unlock()

	for _, req := range requests {
		select {
		case req.resultCh <- DecisionDeny:
		default:
		}
		if g.store != nil {
			g.store.DeletePending(req.ID)
		}
	}
	g.log.Info("all pending approvals cancelled", "reason", reason, "count", len(requests))
}

// BuildApproveFunc creates an ApproveFunc backed by the gate. When gate is nil,
// all tools are auto-approved. Used by the security policy checker.
func BuildApproveFunc(gate *Gate) func(ctx context.Context, toolName, argsJSON, reason string) (string, error) {
	return func(ctx context.Context, toolName, argsJSON, reason string) (string, error) {
		if gate == nil {
			return "approve_once", nil
		}
		decision, _ := gate.RequestApproval(ctx, toolName, argsJSON, reason)
		return decision.String(), nil
	}
}

// ListPendingForUI returns pending approvals formatted for the Wails frontend.
func (g *Gate) ListPendingForUI() []map[string]interface{} {
	pending := g.ListPending()
	result := make([]map[string]interface{}, len(pending))
	for i, p := range pending {
		result[i] = map[string]interface{}{
			"id": p.ID, "toolName": p.ToolName, "args": p.Args,
			"reason": p.Reason, "createdAt": p.CreatedAt, "expiresAt": p.ExpiresAt,
		}
	}
	return result
}

// DecideByBool resolves a pending approval. approve=true maps to DecisionApproveOnce,
// approve=false maps to DecisionDeny.
func (g *Gate) DecideByBool(id string, approve bool) error {
	d := DecisionDeny
	if approve {
		d = DecisionApproveOnce
	}
	return g.Decide(id, d)
}

// ListRecentDecisions returns the most recent audit entries from the store.
func (g *Gate) ListRecentDecisions(limit int) ([]AuditEntry, error) {
	if g.store == nil {
		return nil, fmt.Errorf("no audit store configured")
	}
	return g.store.ListRecentDecisions(limit)
}

// ListAllowlist returns the permanent allowlist.
func (g *Gate) ListAllowlist() ([]AllowlistEntry, error) {
	if g.store == nil {
		return nil, fmt.Errorf("no audit store configured")
	}
	return g.store.ListAllowlist()
}

// RemoveAllowlistEntry removes a tool from the permanent allowlist.
func (g *Gate) RemoveAllowlistEntry(toolName string) error {
	if g.store == nil {
		return fmt.Errorf("no audit store configured")
	}
	return g.store.RemoveFromAllowlist(toolName)
}

// ── Internal ─────────────────────────────────────────────────────────────

// toolRiskAtOrBelow returns true if the named tool's effective risk level
// is at or below the given threshold string ("none", "read_only", "write", "execute").
// Uses the registry-backed resolver when available; falls back to heuristics.
func (g *Gate) toolRiskAtOrBelow(toolName string, threshold string) bool {
	riskOrder := map[string]int{
		"none":      0,
		"read_only": 1,
		"write":     2,
		"execute":   3,
		"dangerous": 4,
	}
	thresholdLevel, ok := riskOrder[threshold]
	if !ok {
		return false
	}

	toolLevel := g.resolveToolRisk(toolName)
	return toolLevel <= thresholdLevel
}

// resolveToolRisk maps a tool name to a risk level integer. Prefers the
// registry-backed resolver when available; falls back to naming heuristics.
func (g *Gate) resolveToolRisk(toolName string) int {
	g.mu.Lock()
	resolver := g.resolveTool
	g.mu.Unlock()

	if resolver != nil {
		permLevel, hasExternal := resolver(toolName)
		if permLevel != "" {
			switch permLevel {
			case "none":
				return 0
			case "read_only":
				return 1
			case "write":
				if hasExternal {
					return 3 // write + external = execute-level risk
				}
				return 2
			case "execute", "dangerous":
				return 3
			}
		}
		// Resolver returned no permission level but told us about external effects.
		if hasExternal {
			return 3
		}
	}

	// Conservative fallback heuristic for tools not in the registry.
	return guessToolRiskHeuristic(toolName)
}

// guessToolRiskHeuristic is the fallback when the registry is unavailable.
func guessToolRiskHeuristic(toolName string) int {
	// Known safe tools (read-only or none)
	safePrefixes := []string{
		"read_", "list_", "grep", "glob", "memory_search", "web_search",
		"detect_", "current_time", "workspace_state", "read_diff", "run_linter",
		"image_info", "browser_open", "todo_list",
	}
	for _, p := range safePrefixes {
		if len(toolName) >= len(p) && toolName[:len(p)] == p {
			return 1 // read_only
		}
	}

	// Known write tools
	writePrefixes := []string{
		"write_", "edit_", "update_memory_md", "csv_export", "todo_add",
		"todo_edit", "todo_update", "todo_remove", "git",
	}
	for _, p := range writePrefixes {
		if len(toolName) >= len(p) && toolName[:len(p)] == p {
			return 2 // write
		}
	}

	// Known execute tools
	execTools := map[string]bool{
		"shell": true, "shell_cmd": true,
		"run_tests": true, "browser": true, "http_get": true, "http_post": true,
	}
	if execTools[toolName] {
		return 3 // execute
	}

	// MCP proxy tools — unknown risk, treat as dangerous
	if len(toolName) > 4 && toolName[:4] == "mcp_" {
		return 4 // dangerous
	}

	return 3 // execute (conservative)
}

// toolHasSideEffects returns true when the named tool is known to modify
// external state. Uses the registry-backed resolver when available.
func (g *Gate) toolHasSideEffects(toolName string) bool {
	g.mu.Lock()
	resolver := g.resolveTool
	g.mu.Unlock()

	if resolver != nil {
		_, hasExternal := resolver(toolName)
		return hasExternal
	}

	// Conservative heuristic fallback.
	externalEffectPrefixes := []string{
		"write_file", "edit_file", "shell",
		"http_post", "git", "run_tests", "run_linter", "todo_add",
		"todo_edit", "todo_update", "todo_remove", "memory_save",
		"update_memory_md", "csv_export", "mcp_",
	}
	for _, p := range externalEffectPrefixes {
		if len(toolName) >= len(p) && toolName[:len(p)] == p {
			return true
		}
	}
	return false
}

func (g *Gate) recoverStale() {
	if g.store == nil {
		return
	}
	stale, err := g.store.RecoverStale(time.Now().UTC())
	if err != nil {
		g.log.Warn("failed to recover stale approvals", "error", err)
		return
	}
	for _, req := range stale {
		g.log.Info("cleaned up stale approval", "id", req.ID, "tool", req.ToolName)
	}
}
