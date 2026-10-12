package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// lazyExtensionScript is a minimal JSON-RPC extension used by the LazyProcess
// tests. It appends to $MNEME_TEST_SPAWN_LOG on every process start, records
// every extension.describe probe in $MNEME_TEST_DESCRIBE_LOG and every
// extension.configure payload in $MNEME_TEST_CONFIGURE_LOG (all outside the
// extension directory, so they never perturb a cache key), and answers the
// handshake with the identity "live-ext". The "slow" tool sleeps so a test can
// hold a call in flight.
const lazyExtensionScript = `#!/bin/sh
if [ -n "$MNEME_TEST_SPAWN_LOG" ]; then printf 'start\n' >> "$MNEME_TEST_SPAWN_LOG"; fi
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
  method=$(printf '%s' "$line" | sed -n 's/.*"method":"\([^"]*\)".*/\1/p')
  name=$(printf '%s' "$line" | sed -n 's/.*"name":"\([^"]*\)".*/\1/p')
  case "$method" in
    extension.describe|manifest)
      if [ -n "$MNEME_TEST_DESCRIBE_LOG" ]; then printf 'describe\n' >> "$MNEME_TEST_DESCRIBE_LOG"; fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"name":"live-ext","version":"9.9.9","tools":["echo","slow"],"agent_defs":[],"protocol_min":1}}\n' "$id"
      ;;
    extension.configure)
      if [ -n "$MNEME_TEST_CONFIGURE_LOG" ]; then printf '%s\n' "$line" >> "$MNEME_TEST_CONFIGURE_LOG"; fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"accepted":true}}\n' "$id"
      ;;
    extension.call_tool)
      if [ "$name" = "slow" ]; then sleep 1; fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"success":true,"output":"ok"}}\n' "$id"
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"unknown"}}\n' "$id"
      ;;
  esac
done
`

// writeLazyExtensionScript writes the fixture script and returns its path.
func writeLazyExtensionScript(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixture extension is a POSIX shell script")
	}
	path := filepath.Join(t.TempDir(), "lazy-ext.sh")
	if err := os.WriteFile(path, []byte(lazyExtensionScript), 0o755); err != nil {
		t.Fatalf("write fixture script: %v", err)
	}
	return path
}

// countLoggedLines counts non-empty lines in an append-only log file; a missing
// file counts as zero.
func countLoggedLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	n := 0
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line != "" {
			n++
		}
	}
	return n
}

// waitFor polls cond until it holds, failing the test on timeout.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// countProcessStarts swaps the spawn seam for a counting delegate. It proves
// "was a process spawned" without depending on process timing.
func countProcessStarts(t *testing.T) *atomic.Int32 {
	t.Helper()
	restore := StartExtensionProcess
	var starts atomic.Int32
	StartExtensionProcess = func(ctx context.Context, command string, log *slog.Logger) (*ProtoProcess, error) {
		starts.Add(1)
		return restore(ctx, command, log)
	}
	t.Cleanup(func() { StartExtensionProcess = restore })
	return &starts
}

// A declaration must expose its whole tool and agent surface — identical to an
// eager extension's — while spawning nothing.
func TestLazyProcessDeclaresWithoutStarting(t *testing.T) {
	starts := countProcessStarts(t)

	lp := NewLazyProcess(LazyProcessConfig{
		Name:     "declared",
		Command:  "sh /nonexistent-ext.sh",
		Manifest: ProtoManifest{Name: "declared", Version: "0.4.2"},
		Tools: []protoToolDef{{
			Name:            "declared_echo",
			Description:     "echo it back",
			Parameters:      map[string]interface{}{"type": "object"},
			Permission:      "write",
			HasEffects:      true,
			ConcurrencySafe: true,
			TimeoutSecs:     7,
		}},
		Agents: []AgentDef{{ID: "declared-agent", Name: "Declared Agent"}},
		Log:    quietLogger(),
	})

	if lp.Started() {
		t.Fatal("a fresh declaration must not report Started")
	}
	if lp.IsAlive() {
		t.Fatal("a fresh declaration must not be alive")
	}
	ts := lp.Tools()
	if len(ts) != 1 {
		t.Fatalf("Tools() = %d tools, want 1", len(ts))
	}
	pt := ts[0]
	if got := pt.Schema().Name; got != "declared_echo" {
		t.Errorf("schema name = %q, want declared_echo", got)
	}
	if got := pt.Schema().Description; got != "echo it back" {
		t.Errorf("schema description = %q", got)
	}
	if perm, ok := pt.(PermissionedTool); !ok {
		t.Error("protoTool must implement PermissionedTool")
	} else if perm.PermissionLevel() != PermWrite {
		t.Errorf("permission = %v, want write", perm.PermissionLevel())
	}
	if se, ok := pt.(SideEffectTool); !ok || !se.SideEffects() {
		t.Error("side effects must follow the cached definition")
	}
	if cs, ok := pt.(ConcurrencySafeTool); !ok || !cs.ConcurrencySafe() {
		t.Error("concurrency safety must follow the cached definition")
	}
	if th, ok := pt.(TimeoutHintTool); !ok {
		t.Error("protoTool must implement TimeoutHintTool")
	} else if th.TimeoutHint() != 7*time.Second {
		t.Errorf("timeout hint = %v, want 7s", th.TimeoutHint())
	}
	desc, ok := pt.(DescribedTool)
	if !ok {
		t.Fatal("protoTool must implement DescribedTool")
	}
	if got := desc.Descriptor().Version; got != "0.4.2" {
		t.Errorf("descriptor version = %q, want the cached manifest version 0.4.2", got)
	}
	if got := desc.Descriptor().Source; got != SourceExtension {
		t.Errorf("descriptor source = %q, want extension", got)
	}

	agents := lp.Agents()
	if len(agents) != 1 || agents[0].ID != "declared-agent" {
		t.Fatalf("Agents() = %+v, want the single cached agent", agents)
	}
	if lp.InFlight() != 0 {
		t.Errorf("InFlight() = %d, want 0", lp.InFlight())
	}

	// A declared extension has nothing to probe: Ping is a no-op success, never
	// a start.
	if err := lp.Ping(context.Background()); err != nil {
		t.Errorf("Ping on an unstarted declaration = %v, want nil", err)
	}
	if got := starts.Load(); got != 0 {
		t.Fatalf("%d processes spawned for a declaration that was never used", got)
	}
}

// N concurrent first calls must start exactly one process (singleflight) and all
// must complete.
func TestLazyProcessStartsOneProcessForConcurrentFirstCalls(t *testing.T) {
	script := writeLazyExtensionScript(t)
	spawnLog := filepath.Join(t.TempDir(), "spawn.log")
	t.Setenv("MNEME_TEST_SPAWN_LOG", spawnLog)
	starts := countProcessStarts(t)

	lp := NewLazyProcess(LazyProcessConfig{
		Name:     "live-ext",
		Command:  "sh " + script,
		Manifest: ProtoManifest{Name: "live-ext", Version: "9.9.9"},
		Log:      quietLogger(),
	})

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, err := lp.call(context.Background(), "extension.call_tool", protoCallToolParams{Name: "echo"})
			if err != nil {
				errs[i] = err
				return
			}
			var cr protoCallToolResult
			if err := json.Unmarshal(resp.Result, &cr); err != nil {
				errs[i] = err
				return
			}
			if !cr.Success {
				errs[i] = fmt.Errorf("tool reported failure: %s", cr.Error)
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent call %d failed: %v", i, err)
		}
	}
	if got := starts.Load(); got != 1 {
		t.Errorf("spawn seam counted %d starts, want exactly 1", got)
	}
	if got := countLoggedLines(t, spawnLog); got != 1 {
		t.Errorf("extension logged %d process starts, want exactly 1", got)
	}
	if !lp.Started() {
		t.Error("the process must be running after the first calls")
	}
	if got := lp.InFlight(); got != 0 {
		t.Errorf("InFlight() = %d after every call returned, want 0", got)
	}
	if lp.LastUsed().IsZero() {
		t.Error("LastUsed must be recorded")
	}
}

// The saved config must reach the process before the first tool call, and a live
// handshake that disagrees with the cache must win (with a warning).
func TestLazyProcessDeliversConfigAndPrefersLiveManifest(t *testing.T) {
	script := writeLazyExtensionScript(t)
	logDir := t.TempDir()
	configureLog := filepath.Join(logDir, "configure.log")
	describeLog := filepath.Join(logDir, "describe.log")
	t.Setenv("MNEME_TEST_CONFIGURE_LOG", configureLog)
	t.Setenv("MNEME_TEST_DESCRIBE_LOG", describeLog)

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	lp := NewLazyProcess(LazyProcessConfig{
		Name:     "cached-name",
		Command:  "sh " + script,
		Manifest: ProtoManifest{Name: "cached-name", Version: "0.0.1"},
		LoadConfig: func() (map[string]interface{}, error) {
			return map[string]interface{}{"token": "abc"}, nil
		},
		Log: log,
	})

	if _, err := lp.call(context.Background(), "extension.call_tool", protoCallToolParams{Name: "echo"}); err != nil {
		t.Fatalf("call: %v", err)
	}

	if got := countLoggedLines(t, configureLog); got != 1 {
		t.Fatalf("extension.configure delivered %d times, want 1", got)
	}
	cfgData, err := os.ReadFile(configureLog)
	if err != nil {
		t.Fatalf("read configure log: %v", err)
	}
	if !strings.Contains(string(cfgData), `"token":"abc"`) {
		t.Errorf("configure payload = %s, want the saved config", strings.TrimSpace(string(cfgData)))
	}

	// The cache said cached-name@0.0.1; the process says live-ext@9.9.9. The live
	// process is authoritative and the disagreement is logged.
	if got := lp.transportManifest().Name; got != "live-ext" {
		t.Errorf("transport manifest name = %q, want the live name", got)
	}
	if got := lp.ManifestName(); got != "live-ext" {
		t.Errorf("ManifestName() = %q, want the live name", got)
	}
	if !strings.Contains(buf.String(), "differs from the cached handshake") {
		t.Errorf("expected a warning about the manifest mismatch, got:\n%s", buf.String())
	}
}

// The idle decision and call admission must be atomic: an unstarted or freshly
// used process survives, a process past the timeout is stopped and its
// generation cleared, and a process with a call in flight is never touched.
func TestLazyProcessReapIfIdleIsAtomicWithCallAdmission(t *testing.T) {
	script := writeLazyExtensionScript(t)
	t.Setenv("MNEME_TEST_SPAWN_LOG", filepath.Join(t.TempDir(), "spawn.log"))
	starts := countProcessStarts(t)

	lp := NewLazyProcess(LazyProcessConfig{
		Name:     "live-ext",
		Command:  "sh " + script,
		Manifest: ProtoManifest{Name: "live-ext", Version: "9.9.9"},
		Log:      quietLogger(),
	})

	call := func(tool string) error {
		t.Helper()
		_, err := lp.call(context.Background(), "extension.call_tool", protoCallToolParams{Name: tool})
		return err
	}

	// Nothing to reap before the first use, and a non-positive timeout never
	// reaps.
	if stopped, err := lp.ReapIfIdle(time.Now().Add(time.Hour), time.Minute); stopped || err != nil {
		t.Fatalf("ReapIfIdle on an unstarted declaration = (%v, %v), want (false, nil)", stopped, err)
	}
	if err := call("echo"); err != nil {
		t.Fatalf("call: %v", err)
	}
	if stopped, err := lp.ReapIfIdle(time.Now(), time.Minute); stopped || err != nil {
		t.Fatalf("ReapIfIdle on a freshly used process = (%v, %v), want (false, nil)", stopped, err)
	}
	if stopped, _ := lp.ReapIfIdle(time.Now().Add(24*time.Hour), 0); stopped {
		t.Fatal("a zero timeout must disable reaping")
	}
	if !lp.Started() {
		t.Fatal("the process must still be running")
	}

	// Idle past the timeout: stopped, generation cleared.
	stopped, err := lp.ReapIfIdle(time.Now().Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatalf("ReapIfIdle: %v", err)
	}
	if !stopped {
		t.Fatal("an idle process past the timeout must be reaped")
	}
	if lp.Started() {
		t.Fatal("a reaped generation must be cleared")
	}

	// A call in flight keeps the process, however old the last use looks.
	done := make(chan error, 1)
	go func() { done <- call("slow") }()
	waitFor(t, "the slow call to be in flight", func() bool { return lp.Started() && lp.InFlight() == 1 })
	if stopped, err := lp.ReapIfIdle(time.Now().Add(24*time.Hour), time.Millisecond); stopped || err != nil {
		t.Fatalf("ReapIfIdle with a call in flight = (%v, %v), want (false, nil)", stopped, err)
	}
	if !lp.Started() {
		t.Fatal("the process with a call in flight must survive")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("slow call: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("slow call never completed")
	}
	if got := starts.Load(); got != 2 {
		t.Errorf("spawn seam counted %d starts, want 2 (one per generation)", got)
	}
}

// A lazily started process must outlive the call that started it. Tool calls
// carry a timeout context, and binding the subprocess to that context would kill
// the extension the moment the first call returns — the lazy transport would
// then respawn (and reconfigure) it on every single call.
func TestLazyProcessSurvivesInitiatingCallContext(t *testing.T) {
	script := writeLazyExtensionScript(t)
	spawnLog := filepath.Join(t.TempDir(), "spawn.log")
	t.Setenv("MNEME_TEST_SPAWN_LOG", spawnLog)
	starts := countProcessStarts(t)

	lp := NewLazyProcess(LazyProcessConfig{
		Name:     "live-ext",
		Command:  "sh " + script,
		Manifest: ProtoManifest{Name: "live-ext", Version: "9.9.9"},
		Log:      quietLogger(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if _, err := lp.call(ctx, "extension.call_tool", protoCallToolParams{Name: "echo"}); err != nil {
		cancel()
		t.Fatalf("first call: %v", err)
	}
	// The call is over: its context (and the deadline inside it) is gone.
	cancel()

	if !lp.IsAlive() {
		t.Fatal("the process was killed when the initiating call's context ended")
	}
	if _, err := lp.call(context.Background(), "extension.call_tool", protoCallToolParams{Name: "echo"}); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got := starts.Load(); got != 1 {
		t.Errorf("spawn seam counted %d starts, want 1 (the process must be reused)", got)
	}
	if got := countLoggedLines(t, spawnLog); got != 1 {
		t.Errorf("extension started %d times, want 1", got)
	}
}

// Stop leaves the declaration startable: the next call starts a fresh process.
func TestLazyProcessStopLeavesItStartable(t *testing.T) {
	script := writeLazyExtensionScript(t)
	spawnLog := filepath.Join(t.TempDir(), "spawn.log")
	t.Setenv("MNEME_TEST_SPAWN_LOG", spawnLog)

	lp := NewLazyProcess(LazyProcessConfig{
		Name:     "live-ext",
		Command:  "sh " + script,
		Manifest: ProtoManifest{Name: "live-ext", Version: "9.9.9"},
		Log:      quietLogger(),
	})

	call := func() {
		t.Helper()
		if _, err := lp.call(context.Background(), "extension.call_tool", protoCallToolParams{Name: "echo"}); err != nil {
			t.Fatalf("call: %v", err)
		}
	}

	call()
	if !lp.Started() {
		t.Fatal("process should be running after the first call")
	}
	if err := lp.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if lp.Started() {
		t.Fatal("Stop must clear the process generation")
	}
	if got := countLoggedLines(t, spawnLog); got != 1 {
		t.Fatalf("spawn log = %d, want 1 before the second use", got)
	}

	call()
	if !lp.Started() {
		t.Fatal("the call after Stop must start a fresh process")
	}
	if got := countLoggedLines(t, spawnLog); got != 2 {
		t.Fatalf("spawn log = %d, want 2 after the second use", got)
	}
	// Stop is idempotent.
	if err := lp.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	if err := lp.Stop(); err != nil {
		t.Fatalf("third Stop: %v", err)
	}
}

// InFlight must be observable while a call is executing (the idle reaper's
// safety condition) and must fall back to zero afterwards.
func TestLazyProcessTracksInFlightCalls(t *testing.T) {
	script := writeLazyExtensionScript(t)
	t.Setenv("MNEME_TEST_SPAWN_LOG", filepath.Join(t.TempDir(), "spawn.log"))

	lp := NewLazyProcess(LazyProcessConfig{
		Name:     "live-ext",
		Command:  "sh " + script,
		Manifest: ProtoManifest{Name: "live-ext", Version: "9.9.9"},
		Log:      quietLogger(),
	})

	done := make(chan error, 1)
	go func() {
		_, err := lp.call(context.Background(), "extension.call_tool", protoCallToolParams{Name: "slow"})
		done <- err
	}()
	waitFor(t, "the slow call to be in flight", func() bool { return lp.Started() && lp.InFlight() == 1 })
	if !lp.Started() {
		t.Fatal("a call with a process must report Started")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("slow call: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("slow call never completed")
	}
	if got := lp.InFlight(); got != 0 {
		t.Fatalf("InFlight() = %d after the call returned, want 0", got)
	}
}

// Stop racing starts must never leave a process behind or deadlock: whichever
// order the transitions run in, the final Stop leaves nothing running.
func TestLazyProcessStopRacingStartIsSafe(t *testing.T) {
	script := writeLazyExtensionScript(t)
	t.Setenv("MNEME_TEST_SPAWN_LOG", filepath.Join(t.TempDir(), "spawn.log"))
	starts := countProcessStarts(t)

	lp := NewLazyProcess(LazyProcessConfig{
		Name:     "live-ext",
		Command:  "sh " + script,
		Manifest: ProtoManifest{Name: "live-ext", Version: "9.9.9"},
		Log:      quietLogger(),
	})

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 4; j++ {
				// Errors are expected: a concurrent Stop fails in-flight calls.
				_, _ = lp.call(context.Background(), "extension.call_tool", protoCallToolParams{Name: "echo"})
			}
		}()
	}
	stopDone := make(chan struct{})
	go func() {
		defer close(stopDone)
		for i := 0; i < 8; i++ {
			if err := lp.Stop(); err != nil {
				t.Errorf("Stop: %v", err)
				return
			}
		}
	}()

	wg.Wait()
	<-stopDone
	if err := lp.Stop(); err != nil {
		t.Fatalf("final Stop: %v", err)
	}
	if lp.Started() {
		t.Fatal("a final Stop must leave no running process")
	}
	if got := starts.Load(); got == 0 {
		t.Fatal("no process was ever started; the race was not exercised")
	}
}
