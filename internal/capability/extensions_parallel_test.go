package capability

import (
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/simon/mneme/internal/tools"
)

// Every extension is an independent process, so their startup handshakes can
// overlap. This asserts the scheduler actually overlaps them, stays within the
// bound, and launches each extension exactly once.
func TestLaunchExtensionsOverlapsWithinBound(t *testing.T) {
	restore := launchExtensionFn
	t.Cleanup(func() { launchExtensionFn = restore })

	var (
		mu          sync.Mutex
		inFlight    int
		maxInFlight int
		started     = map[string]int{}
	)
	release := make(chan struct{})
	launchExtensionFn = func(_ *CapabilityRegistry, binary string, mf *tools.ExtensionManifest, _ ExtensionConfigLoader, _ *slog.Logger) (*tools.ProtoProcess, error) {
		mu.Lock()
		inFlight++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		started[mf.Name]++
		mu.Unlock()

		// Hold the slot until the test releases it, so concurrency is observable
		// rather than timing-dependent.
		<-release

		mu.Lock()
		inFlight--
		mu.Unlock()
		return nil, nil
	}

	pending := make([]pendingExtension, 0, launchConcurrency*2)
	for i := 0; i < launchConcurrency*2; i++ {
		pending = append(pending, pendingExtension{
			name:     string(rune('a' + i)),
			binary:   "/nonexistent",
			manifest: &tools.ExtensionManifest{Name: string(rune('a' + i))},
		})
	}

	done := make(chan struct{})
	go func() {
		launchExtensions(NewCapabilityRegistry(), pending, nil, ExtensionLoadOptions{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		close(done)
	}()

	// Wait until the pool is saturated, then let everything finish.
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		saturated := maxInFlight >= launchConcurrency
		mu.Unlock()
		if saturated || time.Now().After(deadline) {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	close(release)

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("launchExtensions did not return")
	}

	mu.Lock()
	defer mu.Unlock()
	if maxInFlight < 2 {
		t.Errorf("max in-flight launches = %d; extensions are started serially", maxInFlight)
	}
	if maxInFlight > launchConcurrency {
		t.Errorf("max in-flight launches = %d, above the bound %d", maxInFlight, launchConcurrency)
	}
	if len(started) != len(pending) {
		t.Errorf("started %d extensions, want %d", len(started), len(pending))
	}
	for name, n := range started {
		if n != 1 {
			t.Errorf("extension %q was launched %d times, want exactly 1", name, n)
		}
	}
}

// One extension failing to start must not stop the others.
func TestLaunchExtensionsContinuesAfterFailure(t *testing.T) {
	restore := launchExtensionFn
	t.Cleanup(func() { launchExtensionFn = restore })

	var attempted atomic.Int32
	launchExtensionFn = func(_ *CapabilityRegistry, binary string, mf *tools.ExtensionManifest, _ ExtensionConfigLoader, log *slog.Logger) (*tools.ProtoProcess, error) {
		attempted.Add(1)
		if mf.Name == "broken" {
			return nil, errors.New("requires elevation")
		}
		return nil, nil
	}

	pending := []pendingExtension{
		{name: "ok1", binary: "/x", manifest: &tools.ExtensionManifest{Name: "ok1"}},
		{name: "broken", binary: "/x", manifest: &tools.ExtensionManifest{Name: "broken"}},
		{name: "ok2", binary: "/x", manifest: &tools.ExtensionManifest{Name: "ok2"}},
	}
	launchExtensions(NewCapabilityRegistry(), pending, nil, ExtensionLoadOptions{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if got := attempted.Load(); got != int32(len(pending)) {
		t.Fatalf("attempted %d launches, want %d (a failure must not abort the batch)", got, len(pending))
	}
}

// An empty discovery result must not block or spawn anything.
func TestLaunchExtensionsNoopOnEmpty(t *testing.T) {
	done := make(chan struct{})
	go func() {
		launchExtensions(NewCapabilityRegistry(), nil, nil, ExtensionLoadOptions{}, slog.Default())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("launchExtensions blocked with no extensions to start")
	}
}
