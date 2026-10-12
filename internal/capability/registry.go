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
	mu                *sync.RWMutex
	sets              map[string]*CapabilitySet
	exec              map[string]tools.Tool
	toolOwner         map[string]string
	agents            map[string]*tools.AgentDef
	agentOwner        map[string]string
	mcpClients        map[string]*mcpclient.Client
	extensionProcs    map[string]ExtensionProcess
	extensionMonitors map[string]*extensionMonitor // health monitor per started extension
	idleReaperCancel  context.CancelFunc           // stops the lazy idle reaper
	mcpAudit          MCPAuditLogger
	channels          map[string]*channelEntry // registered channel providers
	disposes          map[string]dispose.Func  // setID → teardown (effect-style unwind)

	// extensionFilter, when non-nil, reports whether an extension is disabled by
	// config and must be skipped at discovery. Bootstrap installs it from the
	// [bundles] disabled id list; the install-time load path (loadExtension)
	// consults it to warn that a just-installed extension will not start at the
	// next boot. The type and its accessors live in extensions.go.
	extensionFilter ExtensionFilter
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
		extensionProcs:    make(map[string]ExtensionProcess),
		extensionMonitors: make(map[string]*extensionMonitor),
		channels:          make(map[string]*channelEntry),
		disposes:          make(map[string]dispose.Func),
	}
}

// ── Set management ─────────────────────────────────────

func (r *CapabilityRegistry) AddSet(set *CapabilitySet) error {
	if set == nil {
		return fmt.Errorf("nil capability set")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.sets[set.ID]; exists {
		return fmt.Errorf("set %q already registered", set.ID)
	}
	normalizeSet(set)
	r.sets[set.ID] = set
	return nil
}

// RemoveSet removes a set and everything it owns (tools, agents, MCP clients,
// extension processes) and returns an error when the set does not exist.
// Internally it shares the same idempotent teardown used by extension dispose.
func (r *CapabilityRegistry) RemoveSet(id string) error {
	r.mu.RLock()
	_, exists := r.sets[id]
	r.mu.RUnlock()
	if !exists {
		return fmt.Errorf("set %q not found", id)
	}
	return r.removeSetInternal(id)
}

// replaceSet overwrites an existing set (or inserts it) with the given
// definition. It is the idempotent re-registration path — e.g. reinstalling a
// skill whose set ID already exists, where AddSet would fail.
func (r *CapabilityRegistry) replaceSet(set *CapabilitySet) {
	if set == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	normalizeSet(set)
	r.sets[set.ID] = set
}

// ensureSetLocked returns the set with the given ID, creating it with the
// supplied metadata when absent. It is the single internal primitive behind
// every set-creating registration path. Caller must hold r.mu.
func (r *CapabilityRegistry) ensureSetLocked(id, name string, kind SourceKind) *CapabilitySet {
	if s, ok := r.sets[id]; ok {
		return s
	}
	s := &CapabilitySet{ID: id, Name: name, Kind: kind, Health: HealthOK, Enabled: true}
	normalizeSet(s)
	r.sets[id] = s
	return s
}

// normalizeSet fills in the invariants every CapabilitySet must satisfy:
// non-nil Tools/Agents slices (Go marshals nil as JSON null) and counts that
// match the slices.
func normalizeSet(set *CapabilitySet) {
	if set.Tools == nil {
		set.Tools = make([]ToolDescriptor, 0)
	}
	if set.Agents == nil {
		set.Agents = make([]AgentDescriptor, 0)
	}
	if set.Health == "" {
		set.Health = HealthUnknown
	}
	set.ToolCount = len(set.Tools)
	set.AgentCount = len(set.Agents)
}

// upsertToolDescriptor inserts or replaces a tool descriptor by name.
func upsertToolDescriptor(s *CapabilitySet, desc ToolDescriptor) {
	for i := range s.Tools {
		if s.Tools[i].Name == desc.Name {
			s.Tools[i] = desc
			return
		}
	}
	s.Tools = append(s.Tools, desc)
	s.ToolCount = len(s.Tools)
}

// removeToolDescriptor returns descriptors without the named tool.
func removeToolDescriptor(descs []ToolDescriptor, name string) []ToolDescriptor {
	out := descs[:0]
	for _, d := range descs {
		if d.Name != name {
			out = append(out, d)
		}
	}
	return out
}

// upsertAgentDescriptor inserts or replaces an agent descriptor by ID.
func upsertAgentDescriptor(s *CapabilitySet, desc AgentDescriptor) {
	for i := range s.Agents {
		if s.Agents[i].ID == desc.ID {
			s.Agents[i] = desc
			return
		}
	}
	s.Agents = append(s.Agents, desc)
	s.AgentCount = len(s.Agents)
}

// removeAgentDescriptor returns descriptors without the given agent ID.
func removeAgentDescriptor(descs []AgentDescriptor, id string) []AgentDescriptor {
	out := descs[:0]
	for _, d := range descs {
		if d.ID != id {
			out = append(out, d)
		}
	}
	return out
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
	ids := make([]string, 0, len(r.sets))
	for id, s := range r.sets {
		if s.Kind == kind {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	out := make([]*CapabilitySet, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.sets[id])
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
	if t == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bindToolLocked(setID, t.Schema().Name, t)
}

// bindToolLocked records a tool under setID, moving it out of any previous
// owning set and replacing an existing descriptor with the same name. Caller
// must hold r.mu.
func (r *CapabilityRegistry) bindToolLocked(setID, name string, t tools.Tool) {
	if prevOwner := r.toolOwner[name]; prevOwner != "" && prevOwner != setID {
		slog.Warn("tool name overwritten", "name", name, "prev_owner", prevOwner, "new_owner", setID)
		if prevSet, ok := r.sets[prevOwner]; ok {
			prevSet.Tools = removeToolDescriptor(prevSet.Tools, name)
			prevSet.ToolCount = len(prevSet.Tools)
		}
	}
	r.exec[name] = t
	r.toolOwner[name] = setID
	if s, ok := r.sets[setID]; ok {
		upsertToolDescriptor(s, toolToDescriptor(name, t))
	}
}

func (r *CapabilityRegistry) AllTools() []ToolDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.exec))
	for name := range r.exec {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]ToolDescriptor, 0, len(names))
	for _, name := range names {
		out = append(out, toolToDescriptor(name, r.exec[name]))
	}
	return out
}

// ToolsInSet returns descriptors for the tools owned by the given capability
// set. MCP consumers use this to list a server's tools without assuming any
// naming convention — registration stores tools under their raw names and
// tracks ownership separately.
func (r *CapabilityRegistry) ToolsInSet(setID string) []ToolDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sets[setID]
	if !ok {
		return nil
	}
	out := make([]ToolDescriptor, len(s.Tools))
	copy(out, s.Tools)
	return out
}

func (r *CapabilityRegistry) ToolNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.exec))
	for name := range r.exec {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *CapabilityRegistry) GetTool(name string) (tools.Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.exec[name]
	return t, ok
}

// SetMCPAuditLogger sets the optional MCP audit logger.
func (r *CapabilityRegistry) SetMCPAuditLogger(l MCPAuditLogger) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mcpAudit = l
}

// Execute routes tool execution. Local tools and MCP tools share the same
// path: MCP tools are mcpProxyTool values in the exec map that proxy to
// their server with audit logging.
func (r *CapabilityRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) tools.Result {
	r.mu.RLock()
	t, ok := r.exec[name]
	r.mu.RUnlock()
	if !ok {
		return tools.Result{Error: fmt.Sprintf("tool not found: %s", name)}
	}
	return tools.ExecuteWithTimeout(ctx, t, args, nil)
}

// ── Agent registration ────────────────────────────────

func (r *CapabilityRegistry) RegisterAgent(setID string, def *tools.AgentDef) {
	if def == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bindAgentLocked(setID, def)
}

// bindAgentLocked records an agent definition under setID, moving it out of a
// previous owning set and replacing any descriptor with the same ID (override
// semantics). The set need not exist yet; the definition is still resolvable by
// ID and will be described once the set appears. Caller must hold r.mu.
func (r *CapabilityRegistry) bindAgentLocked(setID string, def *tools.AgentDef) {
	if prevOwner := r.agentOwner[def.ID]; prevOwner != "" && prevOwner != setID {
		if prevSet, ok := r.sets[prevOwner]; ok {
			prevSet.Agents = removeAgentDescriptor(prevSet.Agents, def.ID)
			prevSet.AgentCount = len(prevSet.Agents)
		}
	}
	r.agents[def.ID] = def
	r.agentOwner[def.ID] = setID
	if s, ok := r.sets[setID]; ok {
		upsertAgentDescriptor(s, agentDefToDescriptor(def))
	}
}

func (r *CapabilityRegistry) UnregisterAgent(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.agents, id)
	owner := r.agentOwner[id]
	delete(r.agentOwner, id)
	if s, ok := r.sets[owner]; ok {
		s.Agents = removeAgentDescriptor(s.Agents, id)
		s.AgentCount = len(s.Agents)
	}
}

// RegisterAgentSet registers an agent into its own capability set, creating
// the set on first use. It is used for user-defined agents and agent packs so
// they appear as distinct, named sets rather than being mixed into the Core
// builtin set.
func (r *CapabilityRegistry) RegisterAgentSet(setID string, kind SourceKind, setName string, def *tools.AgentDef) {
	if def == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureSetLocked(setID, setName, kind)
	r.bindAgentLocked(setID, def)
}

func (r *CapabilityRegistry) AllAgents() []AgentDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]AgentDescriptor, 0, len(r.agents))
	for _, def := range r.agents {
		out = append(out, agentDefToDescriptor(def))
	}
	return out
}

func (r *CapabilityRegistry) GetAgent(id string) (*tools.AgentDef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.agents[id]
	return def, ok
}

// ── MCP ───────────────────────────────────────────────

// RegisterMCP registers a tool provided by an external MCP server as an
// mcpProxyTool in the main exec map. The proxy resolves the server's client
// at call time, so tool registration needs only names, and disconnected
// servers simply fail their tools at execution.
func (r *CapabilityRegistry) RegisterMCP(setID, toolName string, client *mcpclient.Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mcpClients[setID] = client
	r.bindToolLocked(setID, toolName, &mcpProxyTool{name: toolName, serverID: setID, reg: r})
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
		// ToolCount is maintained by bindToolLocked; only ConnectedAt needs
		// updating here.
		s.ConnectedAt = now.Format(time.RFC3339)
	}
	r.mu.Unlock()

	return nil
}

func (r *CapabilityRegistry) DisconnectMCPServer(setID string) error {
	r.mu.Lock()
	client := r.mcpClients[setID]
	delete(r.mcpClients, setID)
	var removed []string
	for name, owner := range r.toolOwner {
		if owner == setID {
			removed = append(removed, name)
			delete(r.exec, name)
			delete(r.toolOwner, name)
		}
	}
	if s, ok := r.sets[setID]; ok {
		for _, name := range removed {
			s.Tools = removeToolDescriptor(s.Tools, name)
		}
		s.ToolCount = len(s.Tools)
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
	reaperCancel := r.idleReaperCancel
	r.idleReaperCancel = nil
	disposes := make([]dispose.Func, 0, len(r.disposes))
	for _, d := range r.disposes {
		disposes = append(disposes, d)
	}
	// Copy maps to avoid holding the lock during slow Stop/Close calls.
	extProcs := make(map[string]ExtensionProcess, len(r.extensionProcs))
	mcpClients := make(map[string]*mcpclient.Client, len(r.mcpClients))
	for id, proc := range r.extensionProcs {
		extProcs[id] = proc
	}
	for id, client := range r.mcpClients {
		mcpClients[id] = client
	}
	r.mu.Unlock()

	// Stop the reaper first: it must not start or stop anything while teardown
	// is unwinding the processes.
	if reaperCancel != nil {
		reaperCancel()
	}
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
func (r *CapabilityRegistry) RegisterExtension(setID string, set *CapabilitySet, proc ExtensionProcess, extTools []tools.Tool, extAgents []*tools.AgentDef) (dispose.Func, error) {
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
		if err := r.removeSetInternal(setID); err != nil {
			slog.Warn("extension teardown failed", "set_id", setID, "error", err)
		}
	})

	r.mu.Lock()
	r.disposes[setID] = unwind
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
		}
	}
	for agentID, owner := range r.agentOwner {
		if owner == id {
			delete(r.agents, agentID)
			delete(r.agentOwner, agentID)
		}
	}
	// Channel providers are registered per set too; drop them so a removed
	// set does not leave its providers resolvable forever.
	for name, entry := range r.channels {
		if entry.setID == id {
			delete(r.channels, name)
		}
	}
	mcpClient := r.mcpClients[id]
	delete(r.mcpClients, id)
	extensionProc := r.extensionProcs[id]
	delete(r.extensionProcs, id)
	delete(r.sets, id)
	delete(r.disposes, id)
	r.mu.Unlock()

	// Cancel the health monitor before stopping the process, so a monitor tick
	// cannot restart what teardown is about to unload.
	r.stopExtensionMonitor(id)
	if mcpClient != nil {
		mcpClient.Close()
	}
	if extensionProc != nil {
		extensionProc.Stop()
	}
	return nil
}

// ── Extension process tracking ───────────────────────────────────

// ExtensionProcess is the process-lifetime surface the registry needs from an
// extension: health probing, restart, teardown, and an identity for logs. It is
// satisfied by an eager *tools.ProtoProcess and by a lazily declared
// *tools.LazyProcess, so the health monitor, teardown path and diagnostics do
// not care which kind an extension is.
type ExtensionProcess interface {
	// Started reports whether a process generation exists. A lazily declared
	// extension reports false until its first use and again after the idle
	// reaper stops it; an eager one is always true.
	Started() bool
	IsAlive() bool
	IsCrashed() bool
	Ping(ctx context.Context) error
	Restart(ctx context.Context) error
	Stop() error
	// ManifestName is the extension's name for log messages.
	ManifestName() string
}

// idleReapable is the extra surface a lazily declared extension exposes to the
// registry's idle reaper. Eager *tools.ProtoProcess deliberately does not
// implement it: an eager extension is never reaped.
type idleReapable interface {
	ExtensionProcess
	IsLazy() bool
	// ReapIfIdle atomically decides whether the process is idle and stops it if
	// so; see tools.LazyProcess.ReapIfIdle for the concurrency contract.
	ReapIfIdle(now time.Time, timeout time.Duration) (bool, error)
}

// extensionMonitor owns the health goroutine of one extension generation. Its
// pointer identity lets a goroutine retire its own map entry without racing a
// replacement monitor installed by a later start.
type extensionMonitor struct {
	cancel context.CancelFunc
}

// The health-monitor cadence is a variable pair (rather than constants) so tests
// can watch a monitor tick without sleeping for two minutes. Production never
// reassigns them.
var (
	extensionHealthInterval         = 30 * time.Second
	extensionHealthLivenessInterval = 120 * time.Second
)

// TrackExtension records an extension's process and starts its health monitor
// when there is a process to monitor.
//
// A declared-but-never-started lazy extension gets no monitor at all: there is
// nothing to probe, and a monitor would see IsAlive()==false and spawn the
// process in a restart loop. Such an extension starts its monitor from the lazy
// transport's OnStarted hook (see markExtensionStarted).
func (r *CapabilityRegistry) TrackExtension(setID string, proc ExtensionProcess) {
	r.mu.Lock()
	r.extensionProcs[setID] = proc
	r.mu.Unlock()
	r.startExtensionMonitor(setID, proc)
}

// startExtensionMonitor launches the health goroutine for an extension that has
// a running process. It is a no-op when setID already has a monitor (a restart
// keeps the existing one) or when the process is not started.
func (r *CapabilityRegistry) startExtensionMonitor(setID string, proc ExtensionProcess) {
	if proc == nil || !proc.Started() {
		return
	}
	r.mu.Lock()
	if _, exists := r.extensionMonitors[setID]; exists {
		r.mu.Unlock()
		return
	}
	healthCtx, healthCancel := context.WithCancel(context.Background())
	m := &extensionMonitor{cancel: healthCancel}
	r.extensionMonitors[setID] = m
	r.mu.Unlock()

	go func() {
		r.runExtensionHealthMonitor(healthCtx, setID, proc)
		r.mu.Lock()
		if r.extensionMonitors[setID] == m {
			delete(r.extensionMonitors, setID)
		}
		r.mu.Unlock()
	}()
}

// stopExtensionMonitor cancels and forgets the health monitor for setID. It is
// idempotent and is the teardown half of the monitor lifecycle.
func (r *CapabilityRegistry) stopExtensionMonitor(setID string) {
	r.mu.Lock()
	m := r.extensionMonitors[setID]
	delete(r.extensionMonitors, setID)
	r.mu.Unlock()
	if m != nil {
		m.cancel()
	}
}

// markExtensionStarted records that a lazily declared extension now has a
// running process: its set reports Started, its health is OK again (a previous
// generation may have been declared down), and its health monitor is started
// if it has none.
func (r *CapabilityRegistry) markExtensionStarted(setID string) {
	r.mu.Lock()
	set := r.sets[setID]
	if set != nil {
		set.Started = true
		set.Health = HealthOK
	}
	proc := r.extensionProcs[setID]
	r.mu.Unlock()
	r.startExtensionMonitor(setID, proc)
}

// markExtensionStopped records that a lazily started extension's process is
// gone (reaped or stopped). The health monitor is left running on purpose: it
// skips any extension whose process is not started, so an idle extension is
// never pinged or restarted, and a later start reuses the same monitor.
func (r *CapabilityRegistry) markExtensionStopped(setID string) {
	r.mu.Lock()
	if set := r.sets[setID]; set != nil {
		set.Started = false
	}
	r.mu.Unlock()
}

// runExtensionHealthMonitor probes and auto-restarts one extension process. It
// uses exponential backoff (5s → 10s → 20s → 40s → 80s), max 5 restarts, and
// liveness probes (extension.describe) to detect hung processes. It returns
// when healthCtx is cancelled (teardown) or the restart limit is reached.
func (r *CapabilityRegistry) runExtensionHealthMonitor(healthCtx context.Context, setID string, proc ExtensionProcess) {
	const (
		maxRestarts = 5
		baseDelay   = 5 * time.Second
		maxDelay    = 80 * time.Second
	)
	ticker := time.NewTicker(extensionHealthInterval)
	lastLiveness := time.Now()
	consecutiveFailures := 0
	defer ticker.Stop()
	for {
		select {
		case <-healthCtx.Done():
			return
		case <-ticker.C:
			// A lazily started extension has no process between uses. It is idle
			// by design — not dead — so an unstarted generation must produce no
			// ping, no failure and no restart. The reaper clears Started before
			// it stops the process, so an intentional stop can never be seen
			// here as a crash.
			if !proc.Started() {
				consecutiveFailures = 0
				lastLiveness = time.Now()
				continue
			}

			alive := proc.IsAlive()
			crashed := proc.IsCrashed()

			// Liveness probe: periodically send extension.describe to
			// verify the extension is truly responsive (not hung).
			if alive && !crashed && time.Since(lastLiveness) > extensionHealthLivenessInterval {
				if err := proc.Ping(context.Background()); err != nil {
					slog.Warn("extension liveness probe failed",
						"set_id", setID, "extension", proc.ManifestName(), "error", err)
					crashed = true
				}
				lastLiveness = time.Now()
			}

			if !alive || crashed {
				consecutiveFailures++
				if consecutiveFailures > maxRestarts {
					slog.Error("extension restart limit reached, giving up",
						"set_id", setID, "extension", proc.ManifestName(), "failures", consecutiveFailures)
					r.UpdateSetHealth(setID, HealthDown)
					return
				}
				delay := baseDelay * time.Duration(1<<(consecutiveFailures-1))
				if delay > maxDelay {
					delay = maxDelay
				}
				slog.Warn("extension health check failed, attempting restart",
					"set_id", setID, "extension", proc.ManifestName(),
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
						"set_id", setID, "extension", proc.ManifestName(), "error", err)
					r.UpdateSetHealth(setID, HealthDown)
				} else {
					slog.Info("extension auto-restarted successfully",
						"set_id", setID, "extension", proc.ManifestName())
					r.UpdateSetHealth(setID, HealthOK)
					consecutiveFailures = 0
					lastLiveness = time.Now()
				}
			}
		}
	}
}

// ── Lazy idle reaper ────────────────────────────────────────────

// extensionIdleReapInterval is how often the idle reaper scans for lazy
// processes to stop. It is a variable so tests can reap without waiting 30s.
var extensionIdleReapInterval = 30 * time.Second

// startIdleReaper starts the registry-owned goroutine that stops lazily started
// extensions which have been unused for longer than timeout. A non-positive
// timeout disables reaping entirely. The goroutine is cancelled by Shutdown.
func (r *CapabilityRegistry) startIdleReaper(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	prev := r.idleReaperCancel
	r.idleReaperCancel = cancel
	r.mu.Unlock()
	if prev != nil {
		prev()
	}
	go func() {
		ticker := time.NewTicker(extensionIdleReapInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				r.reapIdleExtensions(now, timeout)
			}
		}
	}()
}

// reapIdleExtensions stops every lazily started extension that has no call in
// flight and has been unused for at least timeout. Stopping leaves the
// declaration intact: the next call starts a fresh process. The idle decision
// and the stop are atomic with respect to call admission (see
// tools.LazyProcess.ReapIfIdle), so a process with an in-flight call — or one
// that just received a call — is never stopped.
func (r *CapabilityRegistry) reapIdleExtensions(now time.Time, timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	type candidate struct {
		id   string
		proc idleReapable
	}
	r.mu.RLock()
	candidates := make([]candidate, 0, len(r.extensionProcs))
	for id, proc := range r.extensionProcs {
		if reapable, ok := proc.(idleReapable); ok {
			candidates = append(candidates, candidate{id: id, proc: reapable})
		}
	}
	r.mu.RUnlock()

	for _, c := range candidates {
		if !c.proc.IsLazy() || !c.proc.Started() {
			continue
		}
		// ReapIfIdle logs the stop at info with the extension and idle time.
		if _, err := c.proc.ReapIfIdle(now, timeout); err != nil {
			slog.Warn("idle extension stop failed", "set_id", c.id, "error", err)
		}
	}
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
