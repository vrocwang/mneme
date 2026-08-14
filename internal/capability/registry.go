package capability

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	mcpclient "github.com/simon/mneme/internal/mcp/client"
	"github.com/simon/mneme/internal/tools"
	"github.com/simon/mneme/pkg/dispose"
)

// MCPAuditLogger is an optional audit logger for MCP tool calls.
// The interface is defined here to avoid circular imports with internal/mcp/audit.
type MCPAuditLogger interface {
	Log(ctx context.Context, server, tool string, args map[string]interface{}, success bool, errMsg string, duration time.Duration) error
}

// CapabilityRegistry is the single source of truth for all tools and agents.
// It replaces tools.Registry + agent.Registry + mcp.Registry + extension loader.
type CapabilityRegistry struct {
	mu                *sync.RWMutex // shared with ScopedView children
	sets              map[string]*CapabilitySet
	exec              map[string]tools.Tool
	toolOwner         map[string]string
	agents            map[string]*tools.AgentDef
	agentOwner        map[string]string
	mcpClients        map[string]*mcpclient.Client
	mcpAuth           mcpclient.AuthProvider // global MCP auth provider (optional)
	extensionProcs    map[string]*tools.ProtoProcess
	extensionMonitors map[string]context.CancelFunc // cancels health monitor goroutine per extension
	mcpToolServer     map[string]string             // toolName → setID
	scopedTools       map[string]bool               // non-nil when this is a ScopedView
	mcpAudit          MCPAuditLogger
	channels          map[string]*channelEntry      // registered channel providers
	disposes          map[string]dispose.Func       // setID → teardown (effect-style unwind)
}

func NewCapabilityRegistry() *CapabilityRegistry {
	return &CapabilityRegistry{
		mu:                &sync.RWMutex{},
		sets:              make(map[string]*CapabilitySet),
		exec:              make(map[string]tools.Tool),
		toolOwner:         make(map[string]string),
		agents:            make(map[string]*tools.AgentDef),
		agentOwner:        make(map[string]string),
		mcpClients:        make(map[string]*mcpclient.Client),
		extensionProcs:    make(map[string]*tools.ProtoProcess),
		extensionMonitors: make(map[string]context.CancelFunc),
		mcpToolServer:     make(map[string]string),
		channels:          make(map[string]*channelEntry),
		disposes:          make(map[string]dispose.Func),
	}
}

// ── Set management ─────────────────────────────────────

func (r *CapabilityRegistry) AddSet(set *CapabilitySet) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.sets[set.ID]; exists {
		return fmt.Errorf("set %q already registered", set.ID)
	}
	// Rebuild Tools/Agents slices from already-registered entries (tools may
	// have been registered before the set was added, as in builtins.go).
	for name, owner := range r.toolOwner {
		if owner == set.ID {
			if t, ok := r.exec[name]; ok {
				set.Tools = append(set.Tools, toolToDescriptor(name, t))
			}
		}
	}
	for id, owner := range r.agentOwner {
		if owner == set.ID {
			if def, ok := r.agents[id]; ok {
				set.Agents = append(set.Agents, agentDefToDescriptor(def))
			}
		}
	}
	for name, owner := range r.mcpToolServer {
		if owner == set.ID {
			set.Tools = append(set.Tools, ToolDescriptor{
				Name:        name,
				Description: "MCP remote tool",
			})
		}
	}
	// Ensure slices are never nil — Go JSON marshals nil as null.
	if set.Tools == nil {
		set.Tools = make([]ToolDescriptor, 0)
	}
	if set.Agents == nil {
		set.Agents = make([]AgentDescriptor, 0)
	}
	set.ToolCount = len(set.Tools)
	set.AgentCount = len(set.Agents)
	if set.Health == "" {
		set.Health = HealthUnknown
	}
	r.sets[set.ID] = set
	return nil
}

// RemoveSet removes a set and everything it owns (tools, agents, MCP clients,
// extension processes). It is the imperative counterpart to DisposeSet and
// returns an error when the set does not exist. Internally it shares the same
// idempotent teardown as dispose-based unwinding.
func (r *CapabilityRegistry) RemoveSet(id string) error {
	r.mu.RLock()
	_, exists := r.sets[id]
	r.mu.RUnlock()
	if !exists {
		return fmt.Errorf("set %q not found", id)
	}
	return r.removeSetInternal(id)
}

func (r *CapabilityRegistry) GetSet(id string) (*CapabilitySet, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sets[id]
	return s, ok
}

func (r *CapabilityRegistry) ListSets() []*CapabilitySet {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.sets))
	for id := range r.sets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*CapabilitySet, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.sets[id])
	}
	return out
}

func (r *CapabilityRegistry) ListSetsByKind(kind SourceKind) []*CapabilitySet {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*CapabilitySet, 0)
	for _, s := range r.sets {
		if s.Kind == kind {
			out = append(out, s)
		}
	}
	return out
}

func (r *CapabilityRegistry) UpdateSetHealth(id string, health SetHealth) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.sets[id]; ok {
		s.Health = health
	}
}

// ── Tool registration ─────────────────────────────────

// RegisterTool registers a local tool implementation (Go function or wrapped
// extension RPC). The tool's Execute method runs in-process.
func (r *CapabilityRegistry) RegisterTool(setID string, t tools.Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := t.Schema().Name
	r.exec[name] = t
	r.addToolToSetLocked(setID, name, toolToDescriptor(name, t))
}

// addToolToSetLocked handles owner tracking, overwrite cleanup, and dedup
// append to the capability set. Caller must hold r.mu.
func (r *CapabilityRegistry) addToolToSetLocked(setID, name string, desc ToolDescriptor) {
	if prevOwner := r.toolOwner[name]; prevOwner != "" && prevOwner != setID {
		slog.Warn("tool name overwritten", "name", name, "prev_owner", prevOwner, "new_owner", setID)
		if prevSet, ok := r.sets[prevOwner]; ok {
			filtered := prevSet.Tools[:0]
			for _, td := range prevSet.Tools {
				if td.Name != name {
					filtered = append(filtered, td)
				}
			}
			prevSet.Tools = filtered
			prevSet.ToolCount = len(filtered)
		}
	}
	r.toolOwner[name] = setID
	if s, ok := r.sets[setID]; ok {
		for _, existing := range s.Tools {
			if existing.Name == name {
				return // already in set
			}
		}
		s.Tools = append(s.Tools, desc)
		s.ToolCount = len(s.Tools)
	}
}

func (r *CapabilityRegistry) UnregisterTool(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.exec, name)
	delete(r.toolOwner, name)
	delete(r.mcpToolServer, name)
}

// UnregisterToolsBySet removes all tools belonging to a capability set.
// Used when a set is dynamically disabled at runtime (e.g. Composio).
func (r *CapabilityRegistry) UnregisterToolsBySet(setID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, owner := range r.toolOwner {
		if owner == setID {
			delete(r.exec, name)
			delete(r.toolOwner, name)
			delete(r.mcpToolServer, name)
		}
	}
	// Clear tool list in the set descriptor.
	if s, ok := r.sets[setID]; ok {
		s.Tools = nil
		s.ToolCount = 0
	}
}

func (r *CapabilityRegistry) AllTools() []ToolDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ToolDescriptor, 0, len(r.exec)+len(r.mcpToolServer))
	for name, t := range r.exec {
		if r.scopedTools != nil && !r.scopedTools[name] {
			continue
		}
		out = append(out, toolToDescriptor(name, t))
	}
	for name := range r.mcpToolServer {
		if r.scopedTools != nil && !r.scopedTools[name] {
			continue
		}
		out = append(out, ToolDescriptor{Name: name, Description: "MCP remote tool"})
	}
	return out
}

func (r *CapabilityRegistry) ToolNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.exec)+len(r.mcpToolServer))
	for name := range r.exec {
		if r.scopedTools != nil && !r.scopedTools[name] {
			continue
		}
		names = append(names, name)
	}
	for name := range r.mcpToolServer {
		if r.scopedTools != nil && !r.scopedTools[name] {
			continue
		}
		names = append(names, name)
	}
	return names
}

func (r *CapabilityRegistry) GetTool(name string) (tools.Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.scopedTools != nil && !r.scopedTools[name] {
		return nil, false
	}
	t, ok := r.exec[name]
	if !ok {
		// Check MCP tools when not found in exec.
		if _, mcpOk := r.mcpToolServer[name]; mcpOk {
			return nil, true // MCP tool exists but execution is handled externally
		}
		return nil, false
	}
	return t, ok
}

// SetMCPAuditLogger sets the optional MCP audit logger.
func (r *CapabilityRegistry) SetMCPAuditLogger(l MCPAuditLogger) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mcpAudit = l
}

// Execute routes tool execution: local tools run directly,
// MCP tools proxy through the MCP client.
func (r *CapabilityRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) tools.Result {
	r.mu.RLock()
	t, ok := r.exec[name]
	if r.scopedTools != nil && !r.scopedTools[name] {
		r.mu.RUnlock()
		return tools.Result{Error: fmt.Sprintf("tool not allowed in scoped view: %s", name)}
	}
	serverID, isMCP := r.mcpToolServer[name]
	client := r.mcpClients[serverID]
	serverName := serverID
	if isMCP {
		if s, ok := r.sets[serverID]; ok {
			serverName = s.Name
		}
	}
	r.mu.RUnlock()

	if isMCP && client != nil {
		start := time.Now()
		result, err := client.CallTool(ctx, name, args)
		elapsed := time.Since(start)

		// Audit log
		if r.mcpAudit != nil {
			success := err == nil && !result.IsError
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			} else if result.IsError {
				errMsg = fmt.Sprintf("%v", result.Content)
			}
			_ = r.mcpAudit.Log(ctx, serverName, name, args, success, errMsg, elapsed)
		}

		if err != nil {
			return tools.Result{Error: err.Error()}
		}
		return resultToToolsResult(result)
	}
	if ok {
		return t.Execute(ctx, args)
	}
	return tools.Result{Error: fmt.Sprintf("tool not found: %s", name)}
}

// ── Agent registration ────────────────────────────────

func (r *CapabilityRegistry) RegisterAgent(setID string, def *tools.AgentDef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[def.ID] = def
	r.agentOwner[def.ID] = setID
	if s, ok := r.sets[setID]; ok {
		s.Agents = append(s.Agents, agentDefToDescriptor(def))
		s.AgentCount = len(s.Agents)
	}
}

func (r *CapabilityRegistry) UnregisterAgent(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.agents, id)
	owner := r.agentOwner[id]
	delete(r.agentOwner, id)
	if s, ok := r.sets[owner]; ok {
		filtered := s.Agents[:0]
		for _, a := range s.Agents {
			if a.ID != id {
				filtered = append(filtered, a)
			}
		}
		s.Agents = filtered
		s.AgentCount = len(filtered)
	}
}

func (r *CapabilityRegistry) AllAgents() []AgentDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]AgentDescriptor, 0, len(r.agents))
	for _, def := range r.agents {
		out = append(out, AgentDescriptor{
			ID:            def.ID,
			Name:          def.Name,
			Description:   def.Description,
			Tier:          def.Tier,
			ToolAllowlist: def.ToolAllowlist,
			ToolDenylist:  def.ToolDenylist,
			MaxIterations: def.MaxIterations,
			Hidden:        def.Hidden,
			Model:         def.Model,
			Temperature:   def.Temperature,
			TimeoutSecs:   def.TimeoutSecs,
			SandboxMode:   def.SandboxMode,
			Background:    def.Background,
		})
	}
	return out
}

func (r *CapabilityRegistry) GetAgent(id string) (*tools.AgentDef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.agents[id]
	return def, ok
}

// ── ScopedView ────────────────────────────────────────

// ScopedView returns a view restricted to the given tool names.
// Shares the parent's execution maps but filters visibility.
func (r *CapabilityRegistry) ScopedView(toolNames []string) *CapabilityRegistry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	filtered := make(map[string]bool, len(toolNames))
	for _, name := range toolNames {
		filtered[name] = true
	}
	return &CapabilityRegistry{
		mu:                r.mu, // share parent mutex for concurrent safety
		exec:              r.exec,
		toolOwner:         r.toolOwner,
		sets:              r.sets,
		agents:            r.agents,
		agentOwner:        r.agentOwner,
		mcpClients:        r.mcpClients,
		extensionProcs:    r.extensionProcs,
		extensionMonitors: r.extensionMonitors,
		mcpToolServer:     r.mcpToolServer,
		scopedTools:       filtered,
	}
}

// ── MCP ───────────────────────────────────────────────

// RegisterMCP registers a tool provided by an external MCP server.
// Unlike RegisterTool, the implementation lives in a remote process and
// is invoked via MCP protocol through the client.
func (r *CapabilityRegistry) RegisterMCP(setID, toolName string, client *mcpclient.Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mcpToolServer[toolName] = setID
	r.mcpClients[setID] = client
	r.addToolToSetLocked(setID, toolName, ToolDescriptor{
		Name:        toolName,
		Description: "MCP remote tool",
	})
}

func (r *CapabilityRegistry) SetMCPAuthProvider(auth mcpclient.AuthProvider) {
	r.mu.Lock()
	r.mcpAuth = auth
	r.mu.Unlock()
}

func (r *CapabilityRegistry) ConnectMCPServer(setID string, entry ServerEntry) error {
	r.mu.RLock()
	_, ok := r.sets[setID]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("set %q not found", setID)
	}

	var client *mcpclient.Client
	switch entry.Transport {
	case "stdio":
		c, err := mcpclient.NewStdio(entry.Name, entry.Command, entry.Args...)
		if err != nil {
			return fmt.Errorf("create MCP client %s: %w", setID, err)
		}
		client = c
	case "http":
		client = mcpclient.NewHTTP(entry.Name, entry.URL)
	default:
		return fmt.Errorf("unknown MCP transport: %s", entry.Transport)
	}

	// Attach the global auth provider if one is set.
	r.mu.RLock()
	auth := r.mcpAuth
	r.mu.RUnlock()
	if auth != nil {
		client = client.WithAuth(auth)
	}

	// Perform MCP handshake for HTTP transports (stdio does it in NewStdio).
	if err := client.Initialize(context.Background()); err != nil {
		client.Close()
		return fmt.Errorf("initialize MCP %s: %w", setID, err)
	}
	mcpTools, err := client.ListTools(context.Background())
	if err != nil {
		client.Close() // prevent subprocess leak on ListTools failure
		return fmt.Errorf("list tools MCP %s: %w", setID, err)
	}

	// Apply per-server tool allow/deny lists.
	mcpTools = filterMCPTools(mcpTools, entry)

	for _, mt := range mcpTools {
		r.RegisterMCP(setID, mt.Name, client)
	}

	now := time.Now()
	r.UpdateSetHealth(setID, HealthOK)
	r.mu.Lock()
	if s, ok := r.sets[setID]; ok {
		s.ConnectedAt = now.Format(time.RFC3339)
		s.ToolCount = len(mcpTools)
	}
	r.mu.Unlock()

	return nil
}

func (r *CapabilityRegistry) DisconnectMCPServer(setID string) error {
	r.mu.Lock()
	client := r.mcpClients[setID]
	delete(r.mcpClients, setID)
	for name, owner := range r.mcpToolServer {
		if owner == setID {
			delete(r.mcpToolServer, name)
			delete(r.toolOwner, name)
			delete(r.exec, name)
		}
	}
	if s, ok := r.sets[setID]; ok {
		s.Health = HealthDown
		s.ConnectedAt = ""
	}
	r.mu.Unlock()

	// Close outside the lock — client.Close() blocks on cmd.Wait() for stdio.
	if client != nil {
		client.Close()
	}
	return nil
}

// ── Generic Connect/Disconnect (kind-agnostic) ──────────

// ConnectSet connects a capability set using its Config, dispatching on Kind.
func (r *CapabilityRegistry) ConnectSet(id string) error {
	set, ok := r.GetSet(id)
	if !ok {
		return fmt.Errorf("set %q not found", id)
	}
	switch set.Kind {
	case KindMCPServer:
		var entry ServerEntry
		if err := json.Unmarshal(set.Config, &entry); err != nil {
			return fmt.Errorf("set %q: invalid MCP config: %w", id, err)
		}
		return r.ConnectMCPServer(id, entry)
	default:
		return fmt.Errorf("set %q kind %q does not support connect", id, set.Kind)
	}
}

// DisconnectSet disconnects a capability set, dispatching on Kind.
func (r *CapabilityRegistry) DisconnectSet(id string) error {
	set, ok := r.GetSet(id)
	if !ok {
		return fmt.Errorf("set %q not found", id)
	}
	switch set.Kind {
	case KindMCPServer:
		return r.DisconnectMCPServer(id)
	default:
		return fmt.Errorf("set %q kind %q does not support disconnect", id, set.Kind)
	}
}

// Shutdown stops all extension processes and disconnects all MCP servers.
// Call during app shutdown to prevent orphaned child processes. It unwinds
// registered effects (dispose funcs) in addition to any process/mcp client
// not yet tracked via RegisterExtension.
func (r *CapabilityRegistry) Shutdown() {
	r.mu.Lock()
	disposes := make([]dispose.Func, 0, len(r.disposes))
	for _, d := range r.disposes {
		disposes = append(disposes, d)
	}
	// Copy maps to avoid holding the lock during slow Stop/Close calls.
	extProcs := make(map[string]*tools.ProtoProcess, len(r.extensionProcs))
	mcpClients := make(map[string]*mcpclient.Client, len(r.mcpClients))
	for id, proc := range r.extensionProcs {
		extProcs[id] = proc
	}
	for id, client := range r.mcpClients {
		mcpClients[id] = client
	}
	r.mu.Unlock()

	// Unwind effect-style registrations first (they stop their own processes).
	for _, d := range disposes {
		d()
	}
	// Fallback for any process/client not covered by a dispose.
	for id, proc := range extProcs {
		slog.Info("stopping extension", "id", id)
		proc.Stop()
	}
	for id, client := range mcpClients {
		slog.Info("disconnecting MCP server", "id", id)
		if err := client.Close(); err != nil {
			slog.Warn("MCP client close error", "id", id, "error", err)
		}
	}
}

// RegisterExtension registers a process-isolated extension as a single effect:
// it registers its tools and agents, tracks its process and health monitor,
// and adds the capability set. It returns a DisposeFunc that unwinds all of
// these in reverse order. On a registration conflict it rolls back any partial
// state and returns an error.
func (r *CapabilityRegistry) RegisterExtension(setID string, set *CapabilitySet, proc *tools.ProtoProcess, extTools []tools.Tool, extAgents []*tools.AgentDef) (dispose.Func, error) {
	// Reserve the set ID first. If it is already taken, return before mutating
	// any tool/agent/process state — this avoids the ownership-overwrite bug
	// where a losing registrar would clobber the winner's tools/agents/process
	// and then its rollback would delete the winner's set.
	if err := r.AddSet(set); err != nil {
		return nil, err
	}

	for _, t := range extTools {
		r.RegisterTool(setID, t)
	}
	for _, a := range extAgents {
		r.RegisterAgent(setID, a)
	}
	r.TrackExtension(setID, proc)

	unwind := dispose.Once(func() {
		_ = r.removeSetInternal(setID)
	})

	r.mu.Lock()
	r.disposes[setID] = unwind
	r.mu.Unlock()
	return unwind, nil
}

// DisposeSet unwinds a previously registered extension effect and removes it
// from the registry. Idempotent: disposing a non-existent or already-disposed
// set is a no-op.
func (r *CapabilityRegistry) DisposeSet(setID string) {
	r.mu.Lock()
	d, ok := r.disposes[setID]
	delete(r.disposes, setID)
	r.mu.Unlock()
	if ok && d != nil {
		d()
	}
}

// RegisterInProcessSet registers a first-party (trusted) capability set that
// lives in-process: its tools and agents are Go values implementing the seam
// interfaces directly, with no subprocess or transport layer. This is the
// in-process twin of RegisterExtension (process-isolated) — both produce the
// same kind of registrations (set + tools + agents) and both return a
// DisposeFunc, so consumers and unload paths treat them uniformly.
func (r *CapabilityRegistry) RegisterInProcessSet(set *CapabilitySet, inTools []tools.Tool, inAgents []*tools.AgentDef) (dispose.Func, error) {
	if set == nil {
		return nil, fmt.Errorf("nil capability set")
	}

	// Reserve the set ID first (same rationale as RegisterExtension): fail
	// before mutating any tool/agent state on a duplicate set ID.
	if err := r.AddSet(set); err != nil {
		return nil, err
	}

	for _, t := range inTools {
		r.RegisterTool(set.ID, t)
	}
	for _, a := range inAgents {
		r.RegisterAgent(set.ID, a)
	}

	unwind := dispose.Once(func() { _ = r.removeSetInternal(set.ID) })
	r.mu.Lock()
	r.disposes[set.ID] = unwind
	r.mu.Unlock()
	return unwind, nil
}

// removeSetInternal performs the full teardown for a set: unregister tools and
// agents, cancel the health monitor, stop the process, and remove the set. It
// is idempotent (missing entries are ignored) and is the shared implementation
// behind RemoveSet and RegisterExtension rollback/dispose.
func (r *CapabilityRegistry) removeSetInternal(id string) error {
	r.mu.Lock()
	if _, exists := r.sets[id]; !exists {
		r.mu.Unlock()
		return nil // already gone — idempotent
	}
	for name, owner := range r.toolOwner {
		if owner == id {
			delete(r.exec, name)
			delete(r.toolOwner, name)
			delete(r.mcpToolServer, name)
		}
	}
	for agentID, owner := range r.agentOwner {
		if owner == id {
			delete(r.agents, agentID)
			delete(r.agentOwner, agentID)
		}
	}
	mcpClient := r.mcpClients[id]
	delete(r.mcpClients, id)
	extensionProc := r.extensionProcs[id]
	delete(r.extensionProcs, id)
	extensionCancel := r.extensionMonitors[id]
	delete(r.extensionMonitors, id)
	delete(r.sets, id)
	delete(r.disposes, id)
	r.mu.Unlock()

	if extensionCancel != nil {
		extensionCancel()
	}
	if mcpClient != nil {
		mcpClient.Close()
	}
	if extensionProc != nil {
		extensionProc.Stop()
	}
	return nil
}

// ── Extension process tracking ───────────────────────────────────

// GetExtensionProcess returns the extension process for a given set ID, or nil.
func (r *CapabilityRegistry) GetExtensionProcess(setID string) *tools.ProtoProcess {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.extensionProcs[setID]
}

func (r *CapabilityRegistry) TrackExtension(setID string, proc *tools.ProtoProcess) {
	healthCtx, healthCancel := context.WithCancel(context.Background())
	r.mu.Lock()
	r.extensionProcs[setID] = proc
	r.extensionMonitors[setID] = healthCancel
	r.mu.Unlock()

	// Start a background health monitor that auto-restarts the extension on crash.
	// Uses exponential backoff (5s → 10s → 20s → 40s → 80s), max 5 restarts,
	// and liveness probes (extension.describe) to detect hung processes.
	// The goroutine exits when healthCtx is cancelled (extension removed from registry).
	const (
		maxRestarts      = 5
		baseDelay        = 5 * time.Second
		maxDelay         = 80 * time.Second
		livenessInterval = 120 * time.Second // full liveness probe every 2 min
	)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		lastLiveness := time.Now()
		consecutiveFailures := 0
		defer ticker.Stop()
		for {
			select {
			case <-healthCtx.Done():
				return
			case <-ticker.C:
				alive := proc.IsAlive()
				crashed := proc.IsCrashed()

				// Liveness probe: periodically send extension.describe to
				// verify the extension is truly responsive (not hung).
				if alive && !crashed && time.Since(lastLiveness) > livenessInterval {
					if err := proc.Ping(context.Background()); err != nil {
						slog.Warn("extension liveness probe failed",
							"set_id", setID, "extension", proc.Manifest.Name, "error", err)
						crashed = true
					}
					lastLiveness = time.Now()
				}

				if !alive || crashed {
					consecutiveFailures++
					if consecutiveFailures > maxRestarts {
						slog.Error("extension restart limit reached, giving up",
							"set_id", setID, "extension", proc.Manifest.Name, "failures", consecutiveFailures)
						r.UpdateSetHealth(setID, HealthDown)
						return
					}
					delay := baseDelay * time.Duration(1<<(consecutiveFailures-1))
					if delay > maxDelay {
						delay = maxDelay
					}
				slog.Warn("extension health check failed, attempting restart",
					"set_id", setID, "extension", proc.Manifest.Name,
					"attempt", consecutiveFailures, "delay", delay)
				// Sleep interruptibly: teardown (removeSetInternal) cancels
				// healthCtx and must be able to stop this goroutine before it
				// calls Restart and resurrects an already-unloaded process.
				select {
				case <-healthCtx.Done():
					return
				case <-time.After(delay):
				}
				if err := proc.Restart(healthCtx); err != nil {
						slog.Error("extension auto-restart failed",
							"set_id", setID, "extension", proc.Manifest.Name, "error", err)
						r.UpdateSetHealth(setID, HealthDown)
					} else {
						slog.Info("extension auto-restarted successfully",
							"set_id", setID, "extension", proc.Manifest.Name)
						r.UpdateSetHealth(setID, HealthOK)
						consecutiveFailures = 0
						lastLiveness = time.Now()
					}
				}
			}
		}
	}()
}

// ── Helpers ───────────────────────────────────────────

// filterMCPTools applies per-server tool allow/deny lists.
// When AllowedTools is non-empty, only listed tools pass through.
// DisallowedTools are always excluded regardless of AllowedTools.
func filterMCPTools(tools []mcpclient.Tool, entry ServerEntry) []mcpclient.Tool {
	if len(entry.AllowedTools) == 0 && len(entry.DisallowedTools) == 0 {
		return tools
	}
	allowed := make(map[string]bool, len(entry.AllowedTools))
	for _, t := range entry.AllowedTools {
		allowed[t] = true
	}
	disallowed := make(map[string]bool, len(entry.DisallowedTools))
	for _, t := range entry.DisallowedTools {
		disallowed[t] = true
	}
	filtered := make([]mcpclient.Tool, 0, len(tools))
	for _, t := range tools {
		if disallowed[t.Name] {
			continue
		}
		if len(allowed) > 0 && !allowed[t.Name] {
			continue
		}
		filtered = append(filtered, t)
	}
	return filtered
}

func toolToDescriptor(name string, t tools.Tool) ToolDescriptor {
	s := t.Schema()
	d := ToolDescriptor{
		Name:        name,
		Description: s.Description,
		InputSchema: schemaToJSON(s.Parameters),
	}
	if pt, ok := t.(tools.PermissionedTool); ok {
		d.Permission = pt.PermissionLevel().String()
	}
	if et, ok := t.(tools.SideEffectTool); ok {
		d.HasSideEffects = et.SideEffects()
	}
	if cs, ok := t.(tools.ConcurrencySafeTool); ok {
		d.IsConcurrencySafe = cs.ConcurrencySafe()
	}
	return d
}

func agentDefToDescriptor(def *tools.AgentDef) AgentDescriptor {
	return AgentDescriptor{
		ID:            def.ID,
		Name:          def.Name,
		Description:   def.Description,
		Tier:          def.Tier,
		ToolAllowlist: def.ToolAllowlist,
		ToolDenylist:  def.ToolDenylist,
		MaxIterations: def.MaxIterations,
		Hidden:        def.Hidden,
		Model:         def.Model,
		Temperature:   def.Temperature,
		TimeoutSecs:   def.TimeoutSecs,
		SandboxMode:   def.SandboxMode,
		Background:    def.Background,
	}
}

func schemaToJSON(v interface{}) json.RawMessage {
	if v == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		slog.Warn("failed to marshal tool schema", "error", err)
		return nil
	}
	return data
}

func resultToToolsResult(r *mcpclient.Result) tools.Result {
	if r == nil {
		return tools.Result{}
	}
	var output string
	if r.Content != nil {
		output = fmt.Sprintf("%v", r.Content)
	}
	return tools.Result{Success: !r.IsError, Output: output}
}
