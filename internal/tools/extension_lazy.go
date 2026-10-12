package tools

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// LazyProcess is the declared-but-not-yet-started handle for one extension.
//
// The tool surface is known from the cached handshake (see extension_cache.go),
// so the registry can register every tool and agent at boot while the process
// itself is absent. The first call starts it; an idle process is stopped again
// by the registry's reaper. LazyProcess keeps exactly one process generation
// alive at a time and satisfies the same extensionTransport seam as an eager
// *ProtoProcess, so a protoTool behaves identically either way.
//
// It is safe for concurrent use. Start and stop transitions are serialised by
// startMu, and every observable field is guarded by mu.
type LazyProcess struct {
	cfg LazyProcessConfig
	log *slog.Logger

	// startMu serialises the start and stop transitions. Because a start holds
	// it for its whole body, a Stop that arrives during a start stops the
	// process that start produced instead of leaving an orphan running.
	startMu sync.Mutex

	mu        sync.Mutex
	proc      *ProtoProcess
	manifest  ProtoManifest // live manifest once started, cached manifest before
	lastUsed  time.Time     // end of the most recent call (or start)
	inFlight  int           // calls currently executing (including their start)
	onStarted func()
	onStopped func()
}

// LazyProcessConfig is the ctx-free description of an extension that is declared
// at boot but whose process starts on first use. Everything the process needs is
// resolved at declaration time; the saved config is loaded per start so a config
// change reaches the next process generation.
type LazyProcessConfig struct {
	// Name is the extension's manifest name, used for logging and as the
	// fallback identity until a live handshake reports its own manifest.
	Name string
	// Command is the resolved binary path or interpreter command, in the form
	// StartProtoFromCommand accepts.
	Command string
	// Manifest is the cached handshake manifest.
	Manifest ProtoManifest
	// Tools and Agents are the cached definitions this declaration registers.
	Tools  []protoToolDef
	Agents []AgentDef
	// Log is the parent logger; the process gets a child tagged with the name.
	Log *slog.Logger
	// LoadConfig returns the saved config for this extension, or nil/empty for
	// none. It is called on every start; a failure is logged and does not block
	// the call (the extension may work with defaults).
	LoadConfig func() (map[string]interface{}, error)
	// CacheDir and CacheKey identify the handshake cache entry this declaration
	// came from. They are recorded for diagnostics.
	CacheDir string
	CacheKey string
}

// NewLazyProcess creates a declaration. It starts nothing: no process exists
// until the first call.
func NewLazyProcess(cfg LazyProcessConfig) *LazyProcess {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	if name := cfg.Name; name != "" {
		log = log.With("extension", name)
	}
	return &LazyProcess{cfg: cfg, log: log, manifest: cfg.Manifest}
}

// SetOnStarted registers a callback invoked after a successful start, once per
// process generation. The registry uses it to mark the capability set as
// running and to start its health monitor. The callback runs without lp.mu
// held but before the start returns to the caller; it must not block.
func (lp *LazyProcess) SetOnStarted(fn func()) {
	lp.mu.Lock()
	lp.onStarted = fn
	lp.mu.Unlock()
}

// SetOnStopped registers a callback invoked after a Stop (including the stop
// half of a Restart). The registry uses it to mark the capability set as no
// longer running. It must not block.
func (lp *LazyProcess) SetOnStopped(fn func()) {
	lp.mu.Lock()
	lp.onStopped = fn
	lp.mu.Unlock()
}

// Tools returns the cached tool definitions bound to this transport. The tools
// are usable immediately; calling one starts the process.
func (lp *LazyProcess) Tools() []Tool {
	out := make([]Tool, 0, len(lp.cfg.Tools))
	for _, td := range lp.cfg.Tools {
		out = append(out, &protoTool{proc: lp, def: td})
	}
	return out
}

// Agents returns the cached agent definitions. They cannot start a process by
// themselves — an agent is a definition, not a call — so they stay usable
// whether or not the extension's process is running.
func (lp *LazyProcess) Agents() []*AgentDef {
	out := make([]*AgentDef, 0, len(lp.cfg.Agents))
	for i := range lp.cfg.Agents {
		def := lp.cfg.Agents[i]
		out = append(out, &def)
	}
	return out
}

// Started reports whether a process generation exists. A declaration that has
// never been used, and one whose idle process was reaped, both report false; the
// next call starts a fresh process.
func (lp *LazyProcess) Started() bool {
	lp.mu.Lock()
	defer lp.mu.Unlock()
	return lp.proc != nil
}

// IsAlive reports whether the current process generation is still running.
func (lp *LazyProcess) IsAlive() bool {
	lp.mu.Lock()
	proc := lp.proc
	lp.mu.Unlock()
	return proc != nil && proc.IsAlive()
}

// IsCrashed reports whether the current process generation died from a
// detected fatal error.
func (lp *LazyProcess) IsCrashed() bool {
	lp.mu.Lock()
	proc := lp.proc
	lp.mu.Unlock()
	return proc != nil && proc.IsCrashed()
}

// IsLazy reports that this transport starts on demand. It is the marker the
// registry's idle reaper looks for; an eager *ProtoProcess does not have it.
func (lp *LazyProcess) IsLazy() bool { return true }

// InFlight returns the number of calls currently executing, including the call
// that is starting the process. The reaper never stops a process with calls in
// flight.
func (lp *LazyProcess) InFlight() int {
	lp.mu.Lock()
	defer lp.mu.Unlock()
	return lp.inFlight
}

// LastUsed returns the time the most recent call finished (or the process
// started, for a process with no completed call yet).
func (lp *LazyProcess) LastUsed() time.Time {
	lp.mu.Lock()
	defer lp.mu.Unlock()
	return lp.lastUsed
}

// ManifestName returns the running process's manifest name when one is
// running, else the cached name.
func (lp *LazyProcess) ManifestName() string {
	if name := lp.transportManifest().Name; name != "" {
		return name
	}
	return lp.name()
}

// transportManifest returns the live manifest once a process has started and
// the cached manifest before that, so a descriptor's version is always defined.
func (lp *LazyProcess) transportManifest() ProtoManifest {
	lp.mu.Lock()
	defer lp.mu.Unlock()
	if lp.proc != nil {
		return lp.manifest
	}
	return lp.cfg.Manifest
}

// ReapIfIdle stops the process when it has no call in flight and has been
// unused for at least timeout, reporting whether it stopped one.
//
// The decision and the claim of the process generation happen in the same
// critical section that call admission (beginCall) uses, so a call can never be
// left holding a process the reaper is stopping: the reaper either observes the
// call as in flight and keeps the process, or claims the generation first and
// the call starts a fresh one. A non-positive timeout never reaps.
func (lp *LazyProcess) ReapIfIdle(now time.Time, timeout time.Duration) (bool, error) {
	if timeout <= 0 {
		return false, nil
	}
	lp.startMu.Lock()
	lp.mu.Lock()
	proc := lp.proc
	if proc == nil || lp.inFlight > 0 || lp.lastUsed.IsZero() || now.Sub(lp.lastUsed) < timeout {
		lp.mu.Unlock()
		lp.startMu.Unlock()
		return false, nil
	}
	// Claim the generation: a call admitted from here on sees no process and
	// starts a new one.
	idle := now.Sub(lp.lastUsed)
	lp.proc = nil
	lp.manifest = lp.cfg.Manifest
	lp.mu.Unlock()
	lp.log.Info("stopping idle extension", "name", lp.name(), "idle", idle.Round(time.Second))

	err := proc.Stop()
	lp.startMu.Unlock()
	if fn := lp.stoppedHook(); fn != nil {
		fn()
	}
	return true, err
}

// Stop terminates the current process, if any, and leaves the declaration
// startable again: the next call starts a fresh process. It is idempotent and
// safe against a concurrent start.
func (lp *LazyProcess) Stop() error {
	lp.startMu.Lock()
	defer lp.startMu.Unlock()
	return lp.stopLocked()
}

// Ping probes the running process. A declared-but-never-started extension has
// nothing to probe — it is idle, not down — so Ping starts nothing and reports
// success.
func (lp *LazyProcess) Ping(ctx context.Context) error {
	lp.mu.Lock()
	proc := lp.proc
	lp.mu.Unlock()
	if proc == nil {
		return nil
	}
	return proc.Ping(ctx)
}

// Restart stops the current process (if running) and starts a new one. The
// declaration is left startable either way.
func (lp *LazyProcess) Restart(ctx context.Context) error {
	lp.startMu.Lock()
	_ = lp.stopLocked()
	_, didStart, err := lp.startLocked(ctx)
	lp.startMu.Unlock()
	if err != nil {
		return err
	}
	if didStart {
		lp.fireStarted()
	}
	return nil
}

// call is the extensionTransport seam: ensure a process, then delegate.
func (lp *LazyProcess) call(ctx context.Context, method string, params interface{}) (*protoResponse, error) {
	lp.beginCall()
	defer lp.endCall()

	proc, err := lp.ensureStarted(ctx)
	if err != nil {
		return nil, err
	}
	return proc.call(ctx, method, params)
}

// beginCall records an executing call before the process is ensured, so the
// reaper can never stop a process out from under a call that is about to start
// or use it.
func (lp *LazyProcess) beginCall() {
	lp.mu.Lock()
	lp.inFlight++
	lp.lastUsed = time.Now()
	lp.mu.Unlock()
}

// endCall records completion; the idle clock starts when the last call returns.
func (lp *LazyProcess) endCall() {
	lp.mu.Lock()
	lp.inFlight--
	lp.lastUsed = time.Now()
	lp.mu.Unlock()
}

// ensureStarted returns a live process, starting one if none is running. The
// start is singleflight: concurrent first calls block on startMu and then
// observe the process the first of them produced, so exactly one process is
// spawned no matter how many calls race.
func (lp *LazyProcess) ensureStarted(ctx context.Context) (*ProtoProcess, error) {
	lp.startMu.Lock()
	proc, didStart, err := lp.startLocked(ctx)
	lp.startMu.Unlock()
	if err != nil {
		return nil, err
	}
	if didStart {
		lp.fireStarted()
	}
	return proc, nil
}

// startLocked starts a process when the current generation is missing or dead.
// didStart reports whether this call performed the start, so the OnStarted hook
// fires exactly once per generation. Caller must hold startMu.
func (lp *LazyProcess) startLocked(ctx context.Context) (*ProtoProcess, bool, error) {
	lp.mu.Lock()
	proc := lp.proc
	lp.mu.Unlock()
	if proc != nil && proc.IsAlive() {
		return proc, false, nil
	}
	if proc != nil {
		// A generation exists but is dead (crash, or an external kill). Release
		// it before installing the replacement.
		_ = lp.stopLocked()
	}

	// The process must outlive the call that happened to start it. Tool calls
	// carry a timeout context (ExecuteWithTimeout), and binding the subprocess
	// to it would kill the extension the moment the first call returns. The
	// start and the config delivery get a context that keeps the caller's
	// values but never its cancellation; both are bounded internally by their
	// own setup budget.
	startCtx := ctx
	if startCtx == nil {
		startCtx = context.Background()
	} else {
		startCtx = context.WithoutCancel(startCtx)
	}
	newProc, err := StartExtensionProcess(startCtx, lp.cfg.Command, lp.log)
	if err != nil {
		return nil, false, fmt.Errorf("start %s: %w", lp.name(), err)
	}

	// The live handshake wins. A mismatch means the cache was stale (the key
	// hashes the manifest and binary bytes, so this needs an out-of-band
	// change); the running process is authoritative for the manifest, and the
	// tool surface stays the cached one because the declaration is already
	// registered.
	live := newProc.transportManifest()
	cached := lp.cfg.Manifest
	if live.Name != "" && (live.Name != cached.Name || live.Version != cached.Version) {
		lp.log.Warn("extension manifest differs from the cached handshake; preferring the running process",
			"cached_name", cached.Name, "cached_version", cached.Version,
			"live_name", live.Name, "live_version", live.Version)
	}

	// Deliver the saved configuration before the first tool call, exactly as the
	// eager path does. Without this, saved per-extension config would never
	// reach a lazily started process.
	if lp.cfg.LoadConfig != nil {
		cfg, cfgErr := lp.cfg.LoadConfig()
		if cfgErr != nil {
			lp.log.Warn("extension config load failed", "name", lp.name(), "error", cfgErr)
		} else if len(cfg) > 0 {
			if err := newProc.Configure(startCtx, cfg); err != nil {
				lp.log.Warn("extension configure failed", "name", lp.name(), "error", err)
			}
		}
	}

	now := time.Now()
	lp.mu.Lock()
	lp.proc = newProc
	lp.manifest = live
	lp.lastUsed = now
	lp.mu.Unlock()
	lp.log.Info("extension started on demand",
		"name", lp.name(), "command", lp.cfg.Command, "cache_key", lp.cfg.CacheKey)
	return newProc, true, nil
}

// stopLocked clears the process generation before physically stopping it, so a
// concurrent observer (the health monitor) sees "not started" rather than "just
// died" and never mistakes an intentional stop for a crash. Caller must hold
// startMu.
func (lp *LazyProcess) stopLocked() error {
	lp.mu.Lock()
	proc := lp.proc
	lp.proc = nil
	lp.manifest = lp.cfg.Manifest
	lp.mu.Unlock()
	if proc == nil {
		return nil
	}
	err := proc.Stop()
	if fn := lp.stoppedHook(); fn != nil {
		fn()
	}
	return err
}

func (lp *LazyProcess) fireStarted() {
	if fn := lp.startedHook(); fn != nil {
		fn()
	}
}

func (lp *LazyProcess) startedHook() func() {
	lp.mu.Lock()
	defer lp.mu.Unlock()
	return lp.onStarted
}

func (lp *LazyProcess) stoppedHook() func() {
	lp.mu.Lock()
	defer lp.mu.Unlock()
	return lp.onStopped
}

func (lp *LazyProcess) name() string {
	if lp.cfg.Name != "" {
		return lp.cfg.Name
	}
	return lp.cfg.Manifest.Name
}
