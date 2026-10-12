package capability

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/simon/mneme/internal/tools"
)

// lazyExtensionScript is a minimal extension process with a complete surface: a
// handshake identity, two tools, one agent, and a "slow" tool that answers after
// a second. Every process start appends to $MNEME_TEST_SPAWN_LOG and every
// extension.describe probe to $MNEME_TEST_DESCRIBE_LOG. The log files live
// outside the extension directory, so writing them never changes a cache key.
func lazyExtensionScript(name string) string {
	return `#!/bin/sh
if [ -n "$MNEME_TEST_SPAWN_LOG" ]; then printf 'start\n' >> "$MNEME_TEST_SPAWN_LOG"; fi
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
  method=$(printf '%s' "$line" | sed -n 's/.*"method":"\([^"]*\)".*/\1/p')
  name=$(printf '%s' "$line" | sed -n 's/.*"name":"\([^"]*\)".*/\1/p')
  case "$method" in
    extension.describe|manifest)
      if [ -n "$MNEME_TEST_DESCRIBE_LOG" ]; then printf 'describe\n' >> "$MNEME_TEST_DESCRIBE_LOG"; fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"name":"` + name + `","version":"0.1.0","tools":["` + name + `_echo","` + name + `_slow"],"agent_defs":["` + name + `-agent"],"protocol_min":1}}\n' "$id"
      ;;
    extension.list_tools)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"` + name + `_echo","description":"echo","parameters":{"type":"object"},"permission":"read-only"},{"name":"` + name + `_slow","description":"slow","parameters":{"type":"object"},"permission":"read-only","timeout_secs":30}]}}\n' "$id"
      ;;
    extension.list_agents)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"agents":[{"id":"` + name + `-agent","name":"` + name + ` agent","tier":"standard","systemPrompt":"be brief","maxIterations":3}]}}\n' "$id"
      ;;
    extension.configure)
      if [ -n "$MNEME_TEST_CONFIGURE_LOG" ]; then printf '%s\n' "$line" >> "$MNEME_TEST_CONFIGURE_LOG"; fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"accepted":true}}\n' "$id"
      ;;
    extension.call_tool)
      if [ "$name" = "` + name + `_slow" ]; then sleep 1; fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"success":true,"output":"ok-` + name + `"}}\n' "$id"
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"unknown"}}\n' "$id"
      ;;
  esac
done
`
}

// writeLazyExtension writes <root>/<name>/{manifest.json,ext.sh} and returns the
// extension directory.
func writeLazyExtension(t *testing.T, root, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixture extension is a POSIX shell script")
	}
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ext.sh"), []byte(lazyExtensionScript(name)), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	manifest := fmt.Sprintf(`{"name":%q,"version":"0.1.0","category":"tools","binary":"sh ext.sh"}`, name)
	if err := os.WriteFile(filepath.Join(dir, tools.ExtensionManifestFile), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return dir
}

// lazyCacheKey recomputes the key the loader will use for an extension written
// by writeLazyExtension.
func lazyCacheKey(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	key, ok := tools.ExtensionCacheKey(dir, filepath.Join(dir, tools.ExtensionManifestFile), "sh "+filepath.Join(dir, "ext.sh"))
	if !ok {
		t.Fatalf("ExtensionCacheKey refused the fixture %q", name)
	}
	return key
}

// countLazyLogLines counts non-empty lines in an append-only log; a missing file
// is zero.
func countLazyLogLines(t *testing.T, path string) int {
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

// waitForLazy polls cond until it holds, failing the test on timeout.
func waitForLazy(t *testing.T, what string, cond func() bool) {
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

// lazyTransport returns the *tools.LazyProcess declared for name.
func lazyTransport(t *testing.T, reg *CapabilityRegistry, name string) *tools.LazyProcess {
	t.Helper()
	reg.mu.RLock()
	proc := reg.extensionProcs[ExtensionSetID(name)]
	reg.mu.RUnlock()
	lp, ok := proc.(*tools.LazyProcess)
	if !ok {
		t.Fatalf("extension %q is %T, want *tools.LazyProcess", name, proc)
	}
	return lp
}

// monitorCount reports how many health monitors the registry owns. A
// declared-but-unstarted lazy extension must contribute none.
func monitorCount(reg *CapabilityRegistry) int {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return len(reg.extensionMonitors)
}

// requireSetFlags asserts the extension-only Started/Lazy flags the RPC layer
// exposes. Both are read under the registry lock: the idle reaper writes
// Started from its own goroutine, so reading the live set pointer unlocked
// would be a data race.
func requireSetFlags(t *testing.T, reg *CapabilityRegistry, name string, wantStarted, wantLazy bool) {
	t.Helper()
	reg.mu.RLock()
	set, ok := reg.sets[ExtensionSetID(name)]
	var started, lazy bool
	if ok {
		started, lazy = set.Started, set.Lazy
	}
	reg.mu.RUnlock()
	if !ok {
		t.Fatalf("set %q is not registered", ExtensionSetID(name))
	}
	if started != wantStarted || lazy != wantLazy {
		t.Fatalf("set %q flags = started:%v lazy:%v, want started:%v lazy:%v",
			name, started, lazy, wantStarted, wantLazy)
	}
}

// extensionSetState snapshots the fields the lazy tests inspect, under the
// registry lock for the same reason as requireSetFlags.
func extensionSetState(reg *CapabilityRegistry, name string) (present, started, lazy bool, health SetHealth) {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	set, ok := reg.sets[ExtensionSetID(name)]
	if !ok {
		return false, false, false, ""
	}
	return true, set.Started, set.Lazy, set.Health
}

// TestDiscoverExtensionsDeclaresLazilyFromWarmCache is the core contract: an
// eager boot records the handshake, and the next boot declares the same
// extension — complete tool and agent surface, healthy — without starting it.
func TestDiscoverExtensionsDeclaresLazilyFromWarmCache(t *testing.T) {
	root := t.TempDir()
	cacheDir := t.TempDir()
	writeLazyExtension(t, root, "lazyone")
	spawnLog := filepath.Join(t.TempDir(), "spawn.log")
	t.Setenv("MNEME_TEST_SPAWN_LOG", spawnLog)
	log, buf := testLogger()

	// Boot 1: eager, which warms the cache.
	reg1 := NewCapabilityRegistry()
	discoverExtensions(reg1, []string{root}, nil, nil, ExtensionLoadOptions{CacheDir: cacheDir}, log)
	requireSetFlags(t, reg1, "lazyone", true, false)
	if _, ok := reg1.GetTool("lazyone_echo"); !ok {
		t.Fatal("eager boot must register the tool")
	}
	reg1.Shutdown()

	if got := countLazyLogLines(t, spawnLog); got != 1 {
		t.Fatalf("eager boot spawned %d processes, want exactly 1", got)
	}
	key := lazyCacheKey(t, root, "lazyone")
	rec, ok := tools.LoadExtensionCache(cacheDir, key)
	if !ok {
		t.Fatal("eager boot must write a handshake cache record")
	}
	if rec.Manifest.Name != "lazyone" || len(rec.Tools) != 2 || len(rec.Agents) != 1 {
		t.Fatalf("cache record = manifest %q, %d tools, %d agents; want lazyone, 2, 1",
			rec.Manifest.Name, len(rec.Tools), len(rec.Agents))
	}

	// Boot 2: lazy. Same surface, zero processes.
	if err := os.Remove(spawnLog); err != nil && !os.IsNotExist(err) {
		t.Fatalf("reset spawn log: %v", err)
	}
	reg2 := NewCapabilityRegistry()
	defer reg2.Shutdown()
	discoverExtensions(reg2, []string{root}, nil, nil, ExtensionLoadOptions{Lazy: true, CacheDir: cacheDir}, log)

	requireSetFlags(t, reg2, "lazyone", false, true)
	if _, _, _, health := extensionSetState(reg2, "lazyone"); health != HealthOK {
		t.Errorf("a declared-but-idle extension must be healthy, got %q", health)
	}
	if _, ok := reg2.GetTool("lazyone_echo"); !ok {
		t.Error("lazy declaration must expose the cached tool surface")
	}
	if _, ok := reg2.GetTool("lazyone_slow"); !ok {
		t.Error("lazy declaration must expose every cached tool")
	}
	if _, ok := reg2.GetAgent("lazyone-agent"); !ok {
		t.Error("lazy declaration must expose the cached agents")
	}
	if got := countLazyLogLines(t, spawnLog); got != 0 {
		t.Errorf("lazy boot spawned %d processes, want 0", got)
	}
	if got := monitorCount(reg2); got != 0 {
		t.Errorf("%d health monitors exist for an unstarted lazy extension, want 0", got)
	}
	if !strings.Contains(buf.String(), "extension declared lazily") {
		t.Errorf("expected a lazy-declaration log line, got:\n%s", buf.String())
	}
}

// TestLazyExtensionFirstUseStartsExactlyOneProcess proves singleflight through
// the registry's real Execute path: N concurrent first calls, one process, all
// calls complete.
func TestLazyExtensionFirstUseStartsExactlyOneProcess(t *testing.T) {
	root := t.TempDir()
	cacheDir := t.TempDir()
	writeLazyExtension(t, root, "lazyone")
	spawnLog := filepath.Join(t.TempDir(), "spawn.log")
	t.Setenv("MNEME_TEST_SPAWN_LOG", spawnLog)
	log, _ := testLogger()

	warm := NewCapabilityRegistry()
	discoverExtensions(warm, []string{root}, nil, nil, ExtensionLoadOptions{CacheDir: cacheDir}, log)
	warm.Shutdown()
	if err := os.Remove(spawnLog); err != nil && !os.IsNotExist(err) {
		t.Fatalf("reset spawn log: %v", err)
	}

	reg := NewCapabilityRegistry()
	defer reg.Shutdown()
	discoverExtensions(reg, []string{root}, nil, nil, ExtensionLoadOptions{Lazy: true, CacheDir: cacheDir}, log)
	lp := lazyTransport(t, reg, "lazyone")

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res := reg.Execute(context.Background(), "lazyone_echo", map[string]interface{}{})
			if !res.Success {
				errs[i] = fmt.Errorf("tool failed: %s", res.Error)
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent call %d: %v", i, err)
		}
	}
	if got := countLazyLogLines(t, spawnLog); got != 1 {
		t.Errorf("concurrent first calls spawned %d processes, want exactly 1", got)
	}
	if !lp.Started() {
		t.Error("the process must be running after the calls")
	}
	requireSetFlags(t, reg, "lazyone", true, true)
	if got := monitorCount(reg); got != 1 {
		t.Errorf("%d health monitors after the first start, want 1", got)
	}
}

// TestLazyExtensionIdleReapStopsAndRestarts covers the reaper: an idle process
// is stopped, the set reports it, and the next call starts a fresh one.
func TestLazyExtensionIdleReapStopsAndRestarts(t *testing.T) {
	restoreInterval := extensionIdleReapInterval
	extensionIdleReapInterval = 10 * time.Millisecond
	t.Cleanup(func() { extensionIdleReapInterval = restoreInterval })

	workspace := t.TempDir()
	cacheDir := t.TempDir()
	writeLazyExtension(t, filepath.Join(workspace, "extensions"), "lazyone")
	spawnLog := filepath.Join(t.TempDir(), "spawn.log")
	t.Setenv("MNEME_TEST_SPAWN_LOG", spawnLog)
	log, _ := testLogger()

	warm := NewCapabilityRegistry()
	Bootstrap(warm, workspace, nil, nil, nil, ExtensionLoadOptions{CacheDir: cacheDir}, log)
	warm.Shutdown()
	if err := os.Remove(spawnLog); err != nil && !os.IsNotExist(err) {
		t.Fatalf("reset spawn log: %v", err)
	}

	reg := NewCapabilityRegistry()
	defer reg.Shutdown()
	Bootstrap(reg, workspace, nil, nil, nil, ExtensionLoadOptions{
		Lazy:        true,
		IdleTimeout: 500 * time.Millisecond,
		CacheDir:    cacheDir,
	}, log)
	lp := lazyTransport(t, reg, "lazyone")

	res := reg.Execute(context.Background(), "lazyone_echo", map[string]interface{}{})
	if !res.Success {
		t.Fatalf("first call: %s", res.Error)
	}
	if !lp.Started() {
		t.Fatal("the process must be running after the first call")
	}
	if got := countLazyLogLines(t, spawnLog); got != 1 {
		t.Fatalf("spawn log = %d, want 1", got)
	}

	// The reaper clears the process generation first and reports the stop to the
	// registry once the process is gone, so wait for both halves of the state.
	waitForLazy(t, "the idle reaper to stop the process", func() bool {
		present, started, _, _ := extensionSetState(reg, "lazyone")
		return present && !lp.Started() && !started
	})
	requireSetFlags(t, reg, "lazyone", false, true)

	res = reg.Execute(context.Background(), "lazyone_echo", map[string]interface{}{})
	if !res.Success {
		t.Fatalf("call after reap: %s", res.Error)
	}
	if !lp.Started() {
		t.Fatal("a call after a reap must start a fresh process")
	}
	if got := countLazyLogLines(t, spawnLog); got != 2 {
		t.Errorf("spawn log = %d after the restart, want 2", got)
	}
}

// TestLazyExtensionInFlightCallIsNotReaped is the reaper's safety condition: a
// process with a call in flight is never stopped, however idle it looks.
func TestLazyExtensionInFlightCallIsNotReaped(t *testing.T) {
	restoreInterval := extensionIdleReapInterval
	extensionIdleReapInterval = 10 * time.Millisecond
	t.Cleanup(func() { extensionIdleReapInterval = restoreInterval })

	workspace := t.TempDir()
	cacheDir := t.TempDir()
	writeLazyExtension(t, filepath.Join(workspace, "extensions"), "lazyone")
	t.Setenv("MNEME_TEST_SPAWN_LOG", filepath.Join(t.TempDir(), "spawn.log"))
	log, _ := testLogger()

	warm := NewCapabilityRegistry()
	Bootstrap(warm, workspace, nil, nil, nil, ExtensionLoadOptions{CacheDir: cacheDir}, log)
	warm.Shutdown()

	reg := NewCapabilityRegistry()
	defer reg.Shutdown()
	Bootstrap(reg, workspace, nil, nil, nil, ExtensionLoadOptions{
		Lazy:        true,
		IdleTimeout: 30 * time.Millisecond,
		CacheDir:    cacheDir,
	}, log)
	lp := lazyTransport(t, reg, "lazyone")

	done := make(chan tools.Result, 1)
	go func() {
		done <- reg.Execute(context.Background(), "lazyone_slow", map[string]interface{}{})
	}()
	waitForLazy(t, "the slow call to be in flight", func() bool { return lp.Started() && lp.InFlight() == 1 })

	// Several reaper ticks pass while the call is outstanding.
	time.Sleep(200 * time.Millisecond)
	if !lp.Started() {
		t.Fatal("the reaper stopped a process with a call in flight")
	}

	select {
	case res := <-done:
		if !res.Success {
			t.Fatalf("slow call: %s", res.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("slow call never completed")
	}
	// Once the call is done and the process sits idle, reaping resumes.
	waitForLazy(t, "the reaper to stop the now-idle process", func() bool { return !lp.Started() })
}

// TestLazyExtensionUnstartedIsNotPingedOrRestarted is the health invariant: a
// declared-but-never-started extension has no monitor, so it cannot be pinged,
// cannot be restarted, and is never reported down.
func TestLazyExtensionUnstartedIsNotPingedOrRestarted(t *testing.T) {
	restoreHealth := extensionHealthInterval
	restoreLiveness := extensionHealthLivenessInterval
	extensionHealthInterval = 10 * time.Millisecond
	extensionHealthLivenessInterval = time.Millisecond
	t.Cleanup(func() {
		extensionHealthInterval = restoreHealth
		extensionHealthLivenessInterval = restoreLiveness
	})

	root := t.TempDir()
	cacheDir := t.TempDir()
	writeLazyExtension(t, root, "lazyone")
	logDir := t.TempDir()
	spawnLog := filepath.Join(logDir, "spawn.log")
	describeLog := filepath.Join(logDir, "describe.log")
	t.Setenv("MNEME_TEST_SPAWN_LOG", spawnLog)
	t.Setenv("MNEME_TEST_DESCRIBE_LOG", describeLog)
	log, buf := testLogger()

	warm := NewCapabilityRegistry()
	discoverExtensions(warm, []string{root}, nil, nil, ExtensionLoadOptions{CacheDir: cacheDir}, log)
	warm.Shutdown()
	// The warm boot's own start and handshake must not be counted against the
	// lazy declaration below.
	for _, path := range []string{spawnLog, describeLog} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Fatalf("reset %s: %v", path, err)
		}
	}

	reg := NewCapabilityRegistry()
	defer reg.Shutdown()
	discoverExtensions(reg, []string{root}, nil, nil, ExtensionLoadOptions{Lazy: true, CacheDir: cacheDir}, log)

	// Many monitor ticks' worth of wall time: with a monitor, the 10ms ticker
	// plus the 1ms liveness interval would ping and restart immediately.
	time.Sleep(200 * time.Millisecond)

	if got := monitorCount(reg); got != 0 {
		t.Errorf("%d health monitors for a declared-but-unstarted extension, want 0", got)
	}
	if got := countLazyLogLines(t, spawnLog); got != 0 {
		t.Errorf("%d process starts, want 0 (no restart loop)", got)
	}
	if got := countLazyLogLines(t, describeLog); got != 0 {
		t.Errorf("%d extension.describe probes, want 0 (no ping)", got)
	}
	requireSetFlags(t, reg, "lazyone", false, true)
	if _, _, _, health := extensionSetState(reg, "lazyone"); health != HealthOK {
		t.Errorf("health = %q, want %q (an idle lazy extension is not down)", health, HealthOK)
	}
	for _, forbidden := range []string{"health check failed", "auto-restart", "liveness probe failed", "restart limit reached"} {
		if strings.Contains(buf.String(), forbidden) {
			t.Errorf("log contains %q for an unstarted extension:\n%s", forbidden, buf.String())
		}
	}
}

// TestEagerBootLaunchesEveryPendingExtensionAndWritesCache is the lazy=false
// control: behaviour is unchanged (every extension starts at boot) and the cache
// is still written so the next boot can be lazy.
func TestEagerBootLaunchesEveryPendingExtensionAndWritesCache(t *testing.T) {
	root := t.TempDir()
	cacheDir := t.TempDir()
	names := []string{"lazyone", "lazytwo", "lazythree"}
	for _, name := range names {
		writeLazyExtension(t, root, name)
	}
	spawnLog := filepath.Join(t.TempDir(), "spawn.log")
	t.Setenv("MNEME_TEST_SPAWN_LOG", spawnLog)
	log, _ := testLogger()

	reg := NewCapabilityRegistry()
	defer reg.Shutdown()
	discoverExtensions(reg, []string{root}, nil, nil, ExtensionLoadOptions{Lazy: false, CacheDir: cacheDir}, log)

	if got := countLazyLogLines(t, spawnLog); got != len(names) {
		t.Errorf("eager boot spawned %d processes, want %d (one per extension)", got, len(names))
	}
	for _, name := range names {
		requireSetFlags(t, reg, name, true, false)
		if _, ok := reg.GetTool(name + "_echo"); !ok {
			t.Errorf("eager boot did not register %s_echo", name)
		}
		if _, ok := tools.LoadExtensionCache(cacheDir, lazyCacheKey(t, root, name)); !ok {
			t.Errorf("eager boot did not write a cache record for %s", name)
		}
	}
	if got := monitorCount(reg); got != len(names) {
		t.Errorf("%d health monitors for %d eager extensions, want one each", got, len(names))
	}
}

// TestBootstrapThreadsExtensionLoadOptions proves the public entry point the
// boot layer calls really passes lazy/cache options into discovery.
func TestBootstrapThreadsExtensionLoadOptions(t *testing.T) {
	workspace := t.TempDir()
	cacheDir := t.TempDir()
	writeLazyExtension(t, filepath.Join(workspace, "extensions"), "lazyone")
	spawnLog := filepath.Join(t.TempDir(), "spawn.log")
	t.Setenv("MNEME_TEST_SPAWN_LOG", spawnLog)
	log, _ := testLogger()

	warm := NewCapabilityRegistry()
	Bootstrap(warm, workspace, nil, nil, nil, ExtensionLoadOptions{CacheDir: cacheDir}, log)
	warm.Shutdown()
	if err := os.Remove(spawnLog); err != nil && !os.IsNotExist(err) {
		t.Fatalf("reset spawn log: %v", err)
	}

	reg := NewCapabilityRegistry()
	defer reg.Shutdown()
	Bootstrap(reg, workspace, nil, nil, nil, ExtensionLoadOptions{Lazy: true, CacheDir: cacheDir}, log)

	requireSetFlags(t, reg, "lazyone", false, true)
	if got := countLazyLogLines(t, spawnLog); got != 0 {
		t.Errorf("lazy Bootstrap spawned %d processes, want 0", got)
	}
	if _, ok := reg.GetTool("lazyone_echo"); !ok {
		t.Error("lazy Bootstrap must still expose the cached tools")
	}
}
