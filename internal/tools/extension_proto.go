package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/simon/mneme/pkg/extsdk"
)

// ProtocolVersion is the current extension protocol version supported by the host.
const ProtocolVersion = 1

// defaultCallTimeout bounds a host→extension request when the caller supplies a
// context without a deadline.
//
// Today's guarantee comes from the SDK side: pkg/extsdk wraps every
// extension.call_tool handler in a context with extsdk.DefaultCallTimeout (60s,
// overridable by the extension via SetCallTimeout), so a conforming extension
// always answers within its own call timeout. The host applies the same bound so
// that a non-conforming or hung extension fails the call instead of blocking a
// host goroutine forever. Callers that need longer pass a context with a longer
// deadline, which the agent tool runner already does for tool executions.
const defaultCallTimeout = extsdk.DefaultCallTimeout

// setupCallTimeout bounds the startup-phase requests (handshake, tool/agent
// listing, config delivery). These run once, before anything can be slow for
// user-visible reasons, so an extension that takes a while to become ready
// (loading a model, opening a browser) must not be declared broken at the
// per-tool default.
const setupCallTimeout = 3 * time.Minute

// ── Proto manifest ───────────────────────────────────────────────────────────

// ProtoManifest describes a extension binary's metadata. The core reads this
// during the handshake phase after launching the extension subprocess.
type ProtoManifest struct {
	Name         string                 `json:"name"`
	Version      string                 `json:"version"`
	Description  string                 `json:"description,omitempty"`
	Tools        []string               `json:"tools"`                   // tool names provided by this extension
	AgentDefs    []string               `json:"agent_defs"`              // agent IDs provided (optional)
	ConfigSchema map[string]interface{} `json:"config_schema,omitempty"` // JSON Schema for extension settings
	ProtocolMin  int                    `json:"protocol_min"`            // minimum protocol version required
}

// ── Proto JSON-RPC protocol messages ─────────────────────────────────────────

// protoRequest is a JSON-RPC 2.0 request sent to a extension subprocess over stdin.
type protoRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// protoResponse is a JSON-RPC 2.0 response from a extension subprocess on stdout.
type protoResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *protoRPCErr    `json:"error,omitempty"`
}

type protoRPCErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Wire format for each method.

type protoListToolsResult struct {
	Tools []protoToolDef `json:"tools"`
}

type protoToolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Permission  string                 `json:"permission,omitempty"`
	HasEffects  bool                   `json:"has_effects,omitempty"`
	// ConcurrencySafe is declared by the extension (extsdk.ToolDef.ConcurrencySafe).
	// It makes the tool satisfy ConcurrencySafeTool when true; when false the
	// descriptor reports the same "not concurrency safe" default as before.
	ConcurrencySafe bool `json:"concurrency_safe,omitempty"`
	// TimeoutSecs is how long this tool may legitimately run, declared by the
	// extension (extsdk.ToolDef.TimeoutSecs). Zero means "use the global tool
	// timeout"; the host clamps the declared value to MinTimeoutSecs..MaxTimeoutSecs.
	TimeoutSecs int `json:"timeout_secs,omitempty"`
}

type protoCallToolParams struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type protoCallToolResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

// protoConfigureParams is the parameter payload for extension.configure.
type protoConfigureParams struct {
	Config map[string]interface{} `json:"config"`
}

// protoConfigureResult is the extension's answer to extension.configure.
type protoConfigureResult struct {
	Accepted bool   `json:"accepted"`
	Error    string `json:"error,omitempty"`
}

// StartExtensionProcess launches one extension process for an already resolved
// command string (a binary path or an interpreter invocation).
//
// It is the single spawn point shared by the eager loader
// (internal/capability) and by LazyProcess, and it is a variable so a test can
// count spawns — including "was any process started at all for this lazily
// declared extension?" — without depending on timing. Production code never
// reassigns it.
var StartExtensionProcess = StartProtoFromCommand

// StartProtoFromCommand parses a command string that may be a simple
// binary path or an interpreter invocation. When the path points to a
// script file (no execute bit), it reads the shebang line to find the
// interpreter. Falls back to extension-based detection.
func StartProtoFromCommand(ctx context.Context, command string, log *slog.Logger) (*ProtoProcess, error) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty extension command")
	}
	// Multi-word command: interpreter-style (e.g. "python3 script.py").
	if len(parts) > 1 {
		script := parts[len(parts)-1]
		interp := parts[:len(parts)-1]
		return StartProto(ctx, log, script, interp...)
	}
	// Single path.
	path := parts[0]
	if isNativeBinary(path) {
		return StartProto(ctx, log, path)
	}
	// Retry with .exe extension (platform-agnostic: Go on Windows produces .exe,
	// and cross-compiled binaries may be run on any OS).
	if filepath.Ext(path) == "" {
		exePath := path + ".exe"
		if isNativeBinary(exePath) {
			return StartProto(ctx, log, exePath)
		}
	}
	// Script file — resolve interpreter from shebang or extension.
	interp := resolveInterpreter(path)
	if interp == nil {
		return nil, fmt.Errorf("cannot determine interpreter for %q (no shebang, unknown extension)", path)
	}
	return StartProto(ctx, log, path, interp...)
}

// isNativeBinary returns true if path points to a compiled native binary
// (Go, C, Rust, etc.). Detection trusts the file mode and extension rather
// than sniffing magic numbers: the manifest's binary field already declares
// the path, and binaries built by the extension tooling carry an execute bit
// (Unix) or a .exe extension (Windows). Scripts are detected separately via
// shebang/extension in resolveInterpreter.
func isNativeBinary(path string) bool {
	// Check the path as-is first.
	if isNativeBinaryFile(path) {
		return true
	}
	// Try .exe extension (Windows convention, but not universal).
	if filepath.Ext(path) == "" {
		if isNativeBinaryFile(path + ".exe") {
			return true
		}
	}
	return false
}

// isNativeBinaryFile checks a single concrete path for a native binary.
func isNativeBinaryFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}
	// Known executable extension.
	if strings.EqualFold(filepath.Ext(path), ".exe") {
		return true
	}
	// Unix execute bit.
	if info.Mode()&0111 != 0 {
		return true
	}
	// Fall back to magic-number sniffing. This is a defensive catch-all for
	// native binaries that lack an execute bit — notably extensions extracted
	// from the embedded filesystem, where os.WriteFile(0644) drops the mode.
	if info.Size() >= 2048 {
		return hasBinaryMagic(path)
	}
	return false
}

// hasBinaryMagic reads the first 4 bytes of a file and checks for known
// native binary magic numbers: PE (MZ), ELF (\x7fELF), Mach-O variants.
func hasBinaryMagic(path string) bool {
	if isScriptFile(path) {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() < 2048 {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var magic [4]byte
	if _, err := f.Read(magic[:]); err != nil {
		return false
	}
	// PE: MZ (0x4D 0x5A)
	if magic[0] == 0x4D && magic[1] == 0x5A {
		return true
	}
	// ELF: \x7f E L F
	if magic[0] == 0x7F && magic[1] == 'E' && magic[2] == 'L' && magic[3] == 'F' {
		return true
	}
	// Mach-O 32-bit: CE FA ED FE or FE ED FA CE
	if magic[0] == 0xCE && magic[1] == 0xFA && magic[2] == 0xED && magic[3] == 0xFE {
		return true
	}
	if magic[0] == 0xFE && magic[1] == 0xED && magic[2] == 0xFA && magic[3] == 0xCE {
		return true
	}
	// Mach-O 64-bit: CF FA ED FE or FE ED FA CF
	if magic[0] == 0xCF && magic[1] == 0xFA && magic[2] == 0xED && magic[3] == 0xFE {
		return true
	}
	if magic[0] == 0xFE && magic[1] == 0xED && magic[2] == 0xFA && magic[3] == 0xCF {
		return true
	}
	return false
}

// isScriptFile returns true if the file extension suggests a script.
func isScriptFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".py", ".js", ".mjs", ".ts", ".rb", ".sh", ".bash", ".lua", ".pl", ".php":
		return true
	}
	return false
}

// resolveInterpreter reads the shebang line or falls back to extension mapping.
func resolveInterpreter(path string) []string {
	// Try shebang first.
	f, err := os.Open(path)
	if err == nil {
		defer f.Close()
		var first [128]byte
		n, _ := f.Read(first[:])
		// Only the shebang LINE is meaningful. Reading a fixed block and
		// trimming the whole thing would append the first line of script body
		// as interpreter arguments (e.g. "sh" then "while"), so stop at the
		// first newline and ignore a trailing partial line.
		head := string(first[:n])
		if i := strings.IndexByte(head, '\n'); i >= 0 {
			head = head[:i]
		} else if n == len(first) && !strings.Contains(head, "#!") {
			head = ""
		}
		line := strings.TrimSpace(head)
		if strings.HasPrefix(line, "#!") {
			line = strings.TrimPrefix(line, "#!")
			line = strings.TrimSpace(line)
			// Strip /usr/bin/env prefix.
			line = strings.TrimPrefix(line, "/usr/bin/env ")
			line = strings.TrimPrefix(line, "/bin/env ")
			// Split shebang into interpreter + optional args.
			return strings.Fields(line)
		}
	}

	// Extension fallback.
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".py":
		return []string{"python3"}
	case ".js", ".mjs":
		return []string{"node"}
	case ".ts":
		return []string{"npx", "tsx"}
	case ".rb":
		return []string{"ruby"}
	case ".sh", ".bash":
		return []string{"bash"}
	case ".lua":
		return []string{"lua"}
	case ".pl":
		return []string{"perl"}
	case ".php":
		return []string{"php"}
	}
	return nil
}

// ── Response demultiplexer ───────────────────────────────────────────────────

// protoDemux routes JSON-RPC responses read from an extension's stdout to the
// goroutine waiting on the matching request id.
//
// One background reader owns stdout; every call registers a buffered channel
// (capacity 1) before writing its request, so several requests can be in flight
// to the same extension at once. That is what lets the host deliver a cancel
// while a long-running call is still outstanding — the previous implementation
// held a single mutex across the whole write→read cycle and consumed exactly one
// line per call, so two requests could never overlap.
type protoDemux struct {
	mu      sync.Mutex
	pending map[int64]chan *protoResponse
	log     *slog.Logger
}

func newProtoDemux(log *slog.Logger) *protoDemux {
	if log == nil {
		log = slog.Default()
	}
	return &protoDemux{pending: make(map[int64]chan *protoResponse), log: log}
}

// register allocates the delivery channel for id. The one-slot buffer is what
// makes a late response for an abandoned call harmless: the reader can always
// deliver without blocking, and the buffered value is discarded with the channel.
func (d *protoDemux) register(id int64) chan *protoResponse {
	ch := make(chan *protoResponse, 1)
	d.mu.Lock()
	d.pending[id] = ch
	d.mu.Unlock()
	return ch
}

// deregister drops id. Idempotent: cancelling after the reader has already
// delivered is fine, and a later response for the id finds no waiter.
func (d *protoDemux) deregister(id int64) {
	d.mu.Lock()
	delete(d.pending, id)
	d.mu.Unlock()
}

// dispatch parses one stdout line and delivers the response to the waiter
// registered for its id, reporting whether one was found. Malformed lines and
// unsolicited/unknown ids are logged and dropped: the host never expects
// notifications today, and a response for a deregistered (cancelled) call must
// not wake an unrelated waiter or panic.
func (d *protoDemux) dispatch(line []byte) bool {
	var resp protoResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		d.log.Warn("extension stdout line is not valid JSON-RPC", "error", err)
		return false
	}
	d.mu.Lock()
	ch, ok := d.pending[resp.ID]
	d.mu.Unlock()
	if !ok {
		// Also covers id 0 (a notification, or a response with no id): request
		// ids start at 1, so no waiter can exist for it.
		d.log.Warn("dropping unsolicited extension response", "id", resp.ID)
		return false
	}
	select {
	case ch <- &resp:
		return true
	default:
		// A second response for one id: the waiter already has its answer.
		d.log.Warn("dropping duplicate extension response", "id", resp.ID)
		return false
	}
}

// protoTransport is the per-process-generation protocol state: the response
// demultiplexer, the id allocator, the process-exit signal, and the sync
// primitives that must not be copied. Restart swaps this whole value instead of
// copying individual locks, so each process generation starts with a fresh id
// space, a fresh pending map and a fresh exit channel.
type protoTransport struct {
	demux *protoDemux
	log   *slog.Logger

	idMu sync.Mutex
	id   int64

	exit     chan struct{}
	exitOnce sync.Once
	stopOnce sync.Once
	readerWG sync.WaitGroup
}

func newProtoTransport(log *slog.Logger) *protoTransport {
	if log == nil {
		log = slog.Default()
	}
	return &protoTransport{
		demux: newProtoDemux(log),
		log:   log,
		id:    1,
		exit:  make(chan struct{}),
	}
}

// nextID returns the next unused request id.
func (t *protoTransport) nextID() int64 {
	t.idMu.Lock()
	defer t.idMu.Unlock()
	id := t.id
	t.id++
	return id
}

// markExited closes the process-exit channel exactly once. Every pending call
// selects on it, so this is what makes Stop() and process death fail in-flight
// calls promptly instead of leaving them blocked on a dead pipe.
func (t *protoTransport) markExited() {
	t.exitOnce.Do(func() { close(t.exit) })
}

// ── ProtoProcess ─────────────────────────────────────────────────────────────

// ProtoProcess represents a running extension subprocess connected via
// stdin/stdout JSON-RPC. It is the Go-side handle for the subprocess.
//
// Requests are demultiplexed by JSON-RPC id: writes are serialised by writeMu
// (never held while waiting), and a background reader delivers each response to
// the call that registered that id. Several requests can therefore be in flight
// to one extension at a time.
type ProtoProcess struct {
	Manifest ProtoManifest
	toolList []string // local copy of tool names

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	log    *slog.Logger
	cancel context.CancelFunc
	done   chan struct{}

	// writeMu serialises request writes only. It is deliberately not held while
	// waiting for a response (that wait is a select on the demux channel), so a
	// second request can be written while the first is still outstanding.
	writeMu sync.Mutex

	// transport carries the demultiplexer, id allocator and exit signal for the
	// current process generation.
	transport *protoTransport

	// mu guards the mutable process state below and the per-generation handles
	// (cmd, stdin, transport, cancel, done) that Restart replaces.
	mu sync.Mutex

	// Restart fields: stored so the extension can be re-launched after a crash.
	extensionPath string
	extensionArgs []string

	// Crashed is set when stderr monitoring detects a fatal error (panic,
	// fatal, SIGSEGV). Cleared on successful restart.
	crashed    bool
	stderrTail string // last ~1KB of stderr for diagnostics

	// toolDefs/agentDefs keep the raw wire definitions the last
	// extension.list_tools/extension.list_agents call returned. They exist so a
	// running extension's handshake result can be written to the handshake cache
	// (CacheRecord) without exposing the wire types outside this package.
	toolDefs  []protoToolDef
	agentDefs []AgentDef
}

// StartProto launches a extension and performs the handshake. extensionPath is
// the executable (or script). Additional args are passed before the extension
// path when an interpreter is used (e.g. StartProto(ctx, log, "script.py",
// "python3")). For native binaries, omit args.
func StartProto(ctx context.Context, log *slog.Logger, extensionPath string, args ...string) (*ProtoProcess, error) {
	if log == nil {
		log = slog.Default()
	}
	log = log.With("extension", extensionPath)

	// On Windows, binaries without a PATHEXT extension (.exe/.com/.bat/.cmd)
	// cannot be executed by Go's exec.Command. Rename to add .exe.
	if runtime.GOOS == "windows" && len(args) == 0 && filepath.Ext(extensionPath) == "" {
		exePath := extensionPath + ".exe"
		if err := os.Rename(extensionPath, exePath); err == nil {
			extensionPath = exePath
		}
	}
	procCtx, cancel := context.WithCancel(ctx)
	cmdArgs := append(args, extensionPath)
	cmd := exec.CommandContext(procCtx, cmdArgs[0], cmdArgs[1:]...)
	// Windows: prevent extensions from opening visible console windows.
	hideConsoleWindow(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("extension stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("extension stdout pipe: %w", err)
	}
	// Stderr is captured for debugging but not parsed.
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("extension stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("extension start: %w", err)
	}

	pp := &ProtoProcess{
		cmd:           cmd,
		stdin:         stdin,
		log:           log,
		cancel:        cancel,
		done:          make(chan struct{}),
		transport:     newProtoTransport(log),
		extensionPath: extensionPath,
		extensionArgs: args,
	}

	// One background reader owns stdout for this process generation. It
	// demultiplexes responses to their waiters by JSON-RPC id, which is what
	// allows more than one request to be in flight (see protoDemux). It exits on
	// stdout EOF or read error and fails every pending call.
	tr := pp.transport
	tr.readerWG.Add(1)
	go func() {
		defer tr.readerWG.Done()
		pp.readLoop(stdoutPipe, tr)
	}()

	// Drain stderr in the background. Crash keywords (panic, fatal, SIGSEGV)
	// are detected and surfaced as log.Error for alerting. A ring buffer
	// preserves the last ~1KB of stderr for diagnostics.
	ring := make([]byte, 1024)
	var ringPos int
	go func() {
		defer close(pp.done)
		// stderr reaching EOF is NOT proof the process is gone — a scanner error
		// (e.g. one oversized line) or a closed pipe would otherwise kill the
		// transport while the extension is alive and answering on stdout. The
		// stdout reader owns liveness (it fails pending calls on EOF/error).
		scanner := bufio.NewScanner(stderrPipe)
		// Extension stderr is free-form: allow long lines instead of aborting the
		// drain with bufio.ErrTooLong at 64KiB.
		scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for scanner.Scan() {
			line := scanner.Text()
			// Append to ring buffer with line separator.
			entry := line + "\n"
			for i := 0; i < len(entry); i++ {
				ring[ringPos%len(ring)] = entry[i]
				ringPos++
			}

			// Build the current tail string from the ring buffer.
			tail := ringTail(ring, ringPos)
			pp.mu.Lock()
			pp.stderrTail = tail
			pp.mu.Unlock()

			lower := strings.ToLower(line)
			if strings.Contains(lower, "panic:") ||
				strings.Contains(lower, "fatal:") ||
				strings.Contains(lower, "sigsegv") ||
				strings.Contains(lower, "segmentation fault") ||
				strings.Contains(lower, "fatal error") {
				pp.mu.Lock()
				pp.crashed = true
				pp.mu.Unlock()
				log.Error("extension crash detected via stderr",
					"extension", extensionPath,
					"line", line,
					"stderr_tail", truncate(tail, 256))
			} else {
				log.Debug("extension stderr", "line", line)
			}
		}
		if err := scanner.Err(); err != nil {
			log.Warn("extension stderr drain stopped", "extension", extensionPath, "error", err)
		}
		// Process exited — check if it was a crash (non-zero exit).
		if cmd.ProcessState != nil && !cmd.ProcessState.Success() {
			pp.mu.Lock()
			if !pp.crashed {
				pp.crashed = true
			}
			pp.mu.Unlock()
			log.Warn("extension process exited with error",
				"extension", extensionPath,
				"exit_code", cmd.ProcessState.ExitCode(),
				"stderr_tail", truncate(ringTail(ring, ringPos), 256))
		}
	}()

	// Handshake: extension must respond with its manifest.
	setupCtx, setupCancel := setupBudget(ctx)
	defer setupCancel()
	resp, err := pp.call(setupCtx, "extension.describe", nil)
	if err != nil {
		pp.Stop()
		return nil, fmt.Errorf("extension handshake (extension.describe): %w", err)
	}

	var manifest ProtoManifest
	if err := json.Unmarshal(resp.Result, &manifest); err != nil {
		pp.Stop()
		return nil, fmt.Errorf("extension manifest parse: %w", err)
	}
	if manifest.Name == "" {
		pp.Stop()
		return nil, fmt.Errorf("extension manifest missing name")
	}
	// Enforce ProtocolMin: reject extensions that require a newer protocol
	// version than the host supports.
	if manifest.ProtocolMin > ProtocolVersion {
		pp.Stop()
		return nil, fmt.Errorf("extension requires protocol v%d but host only supports v%d",
			manifest.ProtocolMin, ProtocolVersion)
	}
	pp.Manifest = manifest
	pp.log = log.With("extension_name", manifest.Name)
	pp.log.Info("extension connected", "version", manifest.Version, "tools", manifest.Tools, "protocol_min", manifest.ProtocolMin)

	return pp, nil
}

// readLoop owns stdout for one process generation. It reads line-delimited
// JSON-RPC responses and hands each complete line to the demultiplexer, which
// delivers it to the goroutine waiting on that id. Incomplete trailing data at
// EOF is ignored. Returning marks the process exited, which fails every pending
// call promptly.
func (pp *ProtoProcess) readLoop(r io.Reader, tr *protoTransport) {
	reader := bufio.NewReader(r)
	for {
		line, err := reader.ReadBytes('\n')
		if err == nil && len(bytes.TrimSpace(line)) > 0 {
			tr.demux.dispatch(line)
		}
		if err != nil {
			if err != io.EOF {
				tr.log.Warn("extension stdout read failed", "error", err)
			}
			tr.markExited()
			return
		}
	}
}

// setupBudget bounds a startup-phase call when the caller supplied no deadline.
// The handshake and the tool/agent/config exchange happen once, before anything
// can be slow for user-visible reasons, so they get a longer budget than the
// per-call default: an extension that loads a model or opens a browser at start
// must not be declared broken.
func setupBudget(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, setupCallTimeout)
}

// call sends a JSON-RPC request to the extension and waits for the response
// carrying its id.
//
// The write mutex is held only for the single write of the request line, never
// while waiting, so several calls can be in flight to one extension: the
// background reader (see protoDemux) delivers each response to the goroutine
// that asked for it. That is what makes it possible to deliver a cancel request
// while a long-running call is still outstanding.
//
// ctx bounds the wait. When it carries no deadline, defaultCallTimeout is
// applied — the SDK-side bound (extsdk.DefaultCallTimeout) that today's
// guarantee rests on. On cancellation or process exit the pending id is
// deregistered before returning, so a late response for it is dropped by the
// reader rather than resurrecting this waiter.
func (pp *ProtoProcess) call(ctx context.Context, method string, params interface{}) (*protoResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultCallTimeout)
		defer cancel()
	}

	// Snapshot the handles Restart can swap out.
	pp.mu.Lock()
	tr := pp.transport
	stdin := pp.stdin
	pp.mu.Unlock()
	if tr == nil || stdin == nil {
		return nil, fmt.Errorf("%s: extension not running", method)
	}

	id := tr.nextID()
	ch := tr.demux.register(id)
	// Idempotent: covers every early return, the success path, and the cancel
	// path (a late response then finds no waiter).
	defer tr.demux.deregister(id)

	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		rawParams = b
	}

	reqBytes, err := json.Marshal(protoRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  rawParams,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	pp.writeMu.Lock()
	_, werr := fmt.Fprintf(stdin, "%s\n", reqBytes)
	pp.writeMu.Unlock()
	if werr != nil {
		return nil, fmt.Errorf("write request %s: %w", method, werr)
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("extension error [%d]: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("%s: %w", method, ctx.Err())
	case <-tr.exit:
		return nil, fmt.Errorf("%s: extension process exited", method)
	}
}

// ListTools asks the extension for its tool definitions.
func (pp *ProtoProcess) ListTools(ctx context.Context) ([]Tool, error) {
	setupCtx, setupCancel := setupBudget(ctx)
	defer setupCancel()
	resp, err := pp.call(setupCtx, "extension.list_tools", nil)
	if err != nil {
		return nil, err
	}
	var result protoListToolsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("parse tool list: %w", err)
	}

	tools := make([]Tool, 0, len(result.Tools))
	for _, td := range result.Tools {
		pt := &protoTool{
			proc: pp,
			def:  td,
		}
		tools = append(tools, pt)
		pp.toolList = append(pp.toolList, td.Name)
	}
	// Keep the wire definitions so a successful eager launch can write the
	// handshake cache (CacheRecord) for the next boot's lazy declaration.
	pp.mu.Lock()
	pp.toolDefs = append([]protoToolDef(nil), result.Tools...)
	pp.mu.Unlock()
	return tools, nil
}

// AgentDef mirrors the agent definition structure returned by
// extension.list_agents in extension binaries (e.g. skill-runtime).
type AgentDef struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Tier          string   `json:"tier"`
	SystemPrompt  string   `json:"systemPrompt"`
	ToolAllowlist []string `json:"toolAllowlist"`
	ToolDenylist  []string `json:"toolDenylist,omitempty"`
	MaxIterations int      `json:"maxIterations"`
	Hidden        bool     `json:"hidden"`

	// ForkMode when true means this agent inherits the parent's full tool
	// registry without filtering. Used for agents that need broad access
	// to act on the parent's behalf.
	ForkMode bool `json:"forkMode,omitempty"`

	// Extended fields (migrated from agent.AgentDef).
	Model        string           `json:"model,omitempty"`
	Temperature  float64          `json:"temperature,omitempty"`
	TimeoutSecs  int              `json:"timeoutSecs,omitempty"`
	SandboxMode  string           `json:"sandboxMode,omitempty"`
	ExtraTools   []string         `json:"extraTools,omitempty"`
	Background   bool             `json:"background,omitempty"`
	SkillFilter  string           `json:"skillFilter,omitempty"`
	SubagentRefs []SubagentRef    `json:"subagents,omitempty"`
	ToolPolicy   *ProtoToolPolicy `json:"toolPolicy,omitempty"`
}

// SubagentRef represents an agent this definition can spawn.
type SubagentRef struct {
	AgentID      string `json:"agent_id"`
	SkillsFilter string `json:"skills_filter,omitempty"`
}

// ProtoToolPolicy defines per-tool access control for a extension agent.
type ProtoToolPolicy struct {
	RequireApprovalFor []string `json:"requireApprovalFor,omitempty"`
	DenyTools          []string `json:"denyTools,omitempty"`
	MaxToolRounds      int      `json:"maxToolRounds,omitempty"`
}

type protoListAgentsResult struct {
	Agents []AgentDef `json:"agents"`
}

// ListAgents asks the extension for its agent definitions. Extensions that
// don't implement extension.list_agents return an empty list gracefully.
func (pp *ProtoProcess) ListAgents(ctx context.Context) ([]AgentDef, error) {
	setupCtx, setupCancel := setupBudget(ctx)
	defer setupCancel()
	resp, err := pp.call(setupCtx, "extension.list_agents", nil)
	if err != nil {
		// Extension binaries that don't support extension.list_agents will
		// return an error; treat as empty agent list, not a fatal error.
		pp.log.Debug("extension.list_agents not supported", "extension", pp.Manifest.Name, "error", err)
		return nil, nil
	}
	var result protoListAgentsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		pp.log.Debug("extension.list_agents parse failed", "extension", pp.Manifest.Name, "error", err)
		return nil, nil
	}
	pp.mu.Lock()
	pp.agentDefs = append([]AgentDef(nil), result.Agents...)
	pp.mu.Unlock()
	return result.Agents, nil
}

// Configure sends configuration to the extension. Extensions may accept or
// reject the config based on their ConfigSchema. This is the host half of the
// extension.configure protocol whose extension half is implemented by
// pkg/extsdk's SetConfigHandler; both halves must exist for the documented
// extension-config path to work.
func (pp *ProtoProcess) Configure(ctx context.Context, config map[string]interface{}) error {
	setupCtx, setupCancel := setupBudget(ctx)
	defer setupCancel()
	params := protoConfigureParams{Config: config}
	resp, err := pp.call(setupCtx, "extension.configure", params)
	if err != nil {
		return fmt.Errorf("extension.configure: %w", err)
	}
	var result protoConfigureResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return fmt.Errorf("parse configure result: %w", err)
	}
	if !result.Accepted {
		return fmt.Errorf("extension rejected config: %s", result.Error)
	}
	return nil
}

// Stop terminates the extension subprocess and reaps the child process to
// prevent zombie accumulation.
//
// Pending calls are failed immediately (the process-exit channel is closed
// before the process is even signalled), so a call in flight cannot keep a host
// goroutine blocked. Stop is idempotent: the shutdown body runs once per process
// generation, and Restart installs a fresh generation.
func (pp *ProtoProcess) Stop() error {
	pp.mu.Lock()
	tr := pp.transport
	cmd := pp.cmd
	cancel := pp.cancel
	stdin := pp.stdin
	done := pp.done
	pp.mu.Unlock()

	if tr == nil {
		return nil
	}
	tr.stopOnce.Do(func() {
		tr.markExited() // fail every pending call now
		if cancel != nil {
			cancel()
		}
		if stdin != nil {
			stdin.Close() // stdin EOF: a well-behaved extension exits
		}
		// Give the process a moment to exit gracefully.
		waitCh := done
		if waitCh == nil {
			closed := make(chan struct{})
			close(closed)
			waitCh = closed
		}
		select {
		case <-waitCh:
		case <-time.After(2 * time.Second):
			if cmd != nil && cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		}
		// Reap the child process to release OS resources.
		if cmd != nil {
			_ = cmd.Wait()
		}
		// Join the stdout reader. Closing stdin and killing the process make it
		// exit (EOF/read error); waiting keeps a returning Stop from leaving a
		// goroutine that still touches the pipes. Bounded so a wedged reader
		// cannot hang shutdown.
		readerDone := make(chan struct{})
		go func() {
			tr.readerWG.Wait()
			close(readerDone)
		}()
		select {
		case <-readerDone:
		case <-time.After(2 * time.Second):
			pp.log.Warn("extension stdout reader did not exit after stop",
				"extension", pp.extensionPath)
		}
	})
	return nil
}

// IsAlive returns true if the extension subprocess is still running.
func (pp *ProtoProcess) IsAlive() bool {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	if pp.cmd == nil || pp.cmd.Process == nil {
		return false
	}
	if pp.crashed {
		return false
	}
	// Check if the process is still running.
	select {
	case <-pp.done:
		return false
	default:
		return true
	}
}

// IsCrashed returns true if stderr monitoring detected a fatal error.
func (pp *ProtoProcess) IsCrashed() bool {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	return pp.crashed
}

// transportManifest returns the manifest of the current process generation. It
// is the transport half of the extensionTransport seam (see protoTool): a tool
// reads its descriptor version from whichever process — or lazy declaration —
// is behind it.
func (pp *ProtoProcess) transportManifest() ProtoManifest {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	return pp.Manifest
}

// ManifestName returns the extension's manifest name, for logging. An eager
// process always has one: the handshake rejects a manifest without a name.
func (pp *ProtoProcess) ManifestName() string {
	return pp.transportManifest().Name
}

// Started reports whether the registry should watch this extension as a running
// process. An eager extension is started by construction, so this is always
// true: unlike a lazily declared extension there is no "declared but idle"
// state, and a crashed eager process must still be restarted by the health
// monitor rather than treated as intentionally stopped.
func (pp *ProtoProcess) Started() bool { return true }

// CacheRecord builds the handshake cache record for this running process: the
// manifest plus the definitions the last ListTools/ListAgents calls fetched.
// key is the caller's ExtensionCacheKey. Returns nil when the process has no
// manifest name (nothing worth replaying).
func (pp *ProtoProcess) CacheRecord(key string) *CachedExtension {
	pp.mu.Lock()
	manifest := pp.Manifest
	tools := append([]protoToolDef(nil), pp.toolDefs...)
	agents := append([]AgentDef(nil), pp.agentDefs...)
	pp.mu.Unlock()
	if manifest.Name == "" {
		return nil
	}
	return &CachedExtension{
		Key:      key,
		Manifest: manifest,
		Tools:    tools,
		Agents:   agents,
		CachedAt: time.Now(),
	}
}

// Ping sends a lightweight liveness probe (extension.describe) to verify the
// extension is truly responsive, not just alive at the process level.
// Used by the health monitor to detect hung extensions.
func (pp *ProtoProcess) Ping(ctx context.Context) error {
	_, err := pp.call(ctx, "extension.describe", nil)
	return err
}

// Restart stops the current extension process (if running) and launches a new
// one with the same path and arguments. Returns the new ProtoProcess handle
// (the receiver is updated in-place).
func (pp *ProtoProcess) Restart(ctx context.Context) error {
	pp.Stop()

	newProc, err := StartProto(ctx, pp.log, pp.extensionPath, pp.extensionArgs...)
	if err != nil {
		return fmt.Errorf("extension restart: %w", err)
	}

	// Copy state from the new process into the receiver. The transport (and with
	// it the demultiplexer, id space, exit signal and stop/exit onces) is
	// replaced wholesale, never copied: a new process generation needs a fresh
	// pending map and a live exit channel, and copying sync primitives is
	// exactly the kind of mistake that leaks or double-closes.
	pp.mu.Lock()
	pp.cmd = newProc.cmd
	pp.stdin = newProc.stdin
	pp.transport = newProc.transport
	pp.Manifest = newProc.Manifest
	pp.toolList = newProc.toolList
	pp.cancel = newProc.cancel
	pp.done = newProc.done
	pp.crashed = false
	pp.stderrTail = ""
	pp.mu.Unlock()

	pp.log.Info("extension restarted", "extension", pp.extensionPath, "version", pp.Manifest.Version)
	return nil
}

// ringTail reconstructs the last used bytes of a ring buffer as a string,
// handling wrap-around. ringPos is the total number of bytes written.
func ringTail(ring []byte, ringPos int) string {
	n := len(ring)
	if ringPos < n {
		return string(ring[:ringPos])
	}
	start := ringPos % n
	// Data wraps: [start..n-1] then [0..start-1].
	var b strings.Builder
	b.Grow(n)
	b.Write(ring[start:])
	b.Write(ring[:start])
	return b.String()
}

// truncate returns s truncated to maxLen characters with "..." appended.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ── protoTool — Tool implementation that delegates to a extension subprocess ────

// extensionTransport is what a protoTool needs from whatever is behind it: a
// process that is already running (*ProtoProcess) or one that starts on first
// use (*LazyProcess). Because the seam is this narrow, a tool's schema,
// permission, side effects, concurrency and timeout hint are decided entirely
// by its cached definition and the transport's manifest, never by process
// lifetime — the same tool behaves identically whether its process was started
// at boot or by the call that is using it.
type extensionTransport interface {
	call(ctx context.Context, method string, params interface{}) (*protoResponse, error)
	transportManifest() ProtoManifest
}

type protoTool struct {
	proc extensionTransport
	def  protoToolDef
}

func (pt *protoTool) Schema() Schema {
	return Schema{
		Name:        pt.def.Name,
		Description: pt.def.Description,
		Parameters:  pt.def.Parameters,
	}
}

func (pt *protoTool) Execute(ctx context.Context, args map[string]interface{}) Result {
	params := protoCallToolParams{
		Name: pt.def.Name,
		Args: args,
	}
	resp, err := pt.proc.call(ctx, "extension.call_tool", params)
	if err != nil {
		return Result{Success: false, Error: fmt.Sprintf("extension call: %v", err)}
	}
	var cr protoCallToolResult
	if err := json.Unmarshal(resp.Result, &cr); err != nil {
		return Result{Success: false, Error: fmt.Sprintf("parse extension result: %v", err)}
	}
	return Result{Success: cr.Success, Output: cr.Output, Error: cr.Error}
}

// Proto tools default to execute permission and having external effects
// (fail-closed for security), since we can't introspect the extension binary.
func (pt *protoTool) PermissionLevel() PermissionLevel {
	return ParsePermissionLevel(pt.def.Permission)
}

func (pt *protoTool) SideEffects() bool {
	return pt.def.HasEffects
}

// ConcurrencySafe reports whether the extension declared this tool safe to run
// concurrently (extsdk.ToolDef.ConcurrencySafe → wire field concurrency_safe).
// Extensions that do not declare it keep the previous default of false, so this
// only ever widens what a tool opts into.
func (pt *protoTool) ConcurrencySafe() bool {
	return pt.def.ConcurrencySafe
}

// TimeoutHint reports the per-tool budget the extension declared. Zero means
// no opinion, so the global tool timeout applies.
func (pt *protoTool) TimeoutHint() time.Duration {
	if pt.def.TimeoutSecs <= 0 {
		return 0
	}
	return time.Duration(pt.def.TimeoutSecs) * time.Second
}

func (pt *protoTool) Category() ToolCategory {
	return CategorySkill
}

func (pt *protoTool) Descriptor() ToolDescriptor {
	return ToolDescriptor{
		Name:        pt.def.Name,
		Description: pt.def.Description,
		Source:      SourceExtension,
		Scope:       ScopeAll,
		Category:    CategorySkill,
		Permission:  pt.PermissionLevel(),
		Version:     pt.proc.transportManifest().Version,
		Enabled:     true,
	}
}

// Compile-time checks: both transports satisfy the seam protoTool is built on.
var (
	_ extensionTransport = (*ProtoProcess)(nil)
	_ extensionTransport = (*LazyProcess)(nil)
)

// Compile-time check: protoTool implements extended interfaces.
var (
	_ PermissionedTool    = (*protoTool)(nil)
	_ SideEffectTool      = (*protoTool)(nil)
	_ CategorizedTool     = (*protoTool)(nil)
	_ DescribedTool       = (*protoTool)(nil)
	_ ConcurrencySafeTool = (*protoTool)(nil)
	_ TimeoutHintTool     = (*protoTool)(nil)
)
