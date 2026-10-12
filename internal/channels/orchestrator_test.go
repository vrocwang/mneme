package channels

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"
)

// trackingChannel is a Channel that counts Start/Stop calls. Its Events channel
// is never closed, which exercises the guarantee that a consumer exits on ctx
// cancellation even when the channel does not close its Events channel.
type trackingChannel struct {
	name     string
	events   chan Message
	startErr error
	stopErr  error

	mu     sync.Mutex
	starts int
	stops  int
}

func newTrackingChannel(name string) *trackingChannel {
	return &trackingChannel{name: name, events: make(chan Message)}
}

func (c *trackingChannel) Name() string { return c.name }

func (c *trackingChannel) Start(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.starts++
	return c.startErr
}

func (c *trackingChannel) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stops++
	return c.stopErr
}

func (c *trackingChannel) Events() <-chan Message              { return c.events }
func (c *trackingChannel) Send(context.Context, Message) error { return nil }

func (c *trackingChannel) counts() (starts, stops int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.starts, c.stops
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func newLifecycleOrchestrator(names ...string) (*Orchestrator, map[string]*trackingChannel) {
	orch := NewOrchestrator(quietLogger(), NewDispatcher(quietLogger(), nil))
	chans := make(map[string]*trackingChannel, len(names))
	for _, name := range names {
		ch := newTrackingChannel(name)
		chans[name] = ch
		orch.Register(name, ch)
	}
	return orch, chans
}

// TestOrchestrator_StartAllStopOneKeepsOthers is the core lifecycle regression:
// stopping one channel at runtime must not touch the others.
func TestOrchestrator_StartAllStopOneKeepsOthers(t *testing.T) {
	orch, chans := newLifecycleOrchestrator("a", "b", "c")

	orch.StartAll(context.Background())
	if got := orch.Running(); !equalStrings(got, []string{"a", "b", "c"}) {
		t.Fatalf("Running() after StartAll = %v, want [a b c]", got)
	}

	if err := orch.StopChannel("b"); err != nil {
		t.Fatalf("StopChannel(b) error: %v", err)
	}
	if orch.IsRunning("b") {
		t.Error("b still reported running after StopChannel")
	}
	if got := orch.Running(); !equalStrings(got, []string{"a", "c"}) {
		t.Errorf("Running() after stopping b = %v, want [a c]", got)
	}
	for _, name := range []string{"a", "c"} {
		if !orch.IsRunning(name) {
			t.Errorf("channel %q stopped as a side effect of stopping b", name)
		}
		if _, stops := chans[name].counts(); stops != 0 {
			t.Errorf("channel %q Stop called %d times, want 0", name, stops)
		}
	}
	if _, stops := chans["b"].counts(); stops != 1 {
		t.Errorf("channel b Stop called %d times, want 1", stops)
	}

	orch.StopAll()
	if got := orch.Running(); len(got) != 0 {
		t.Errorf("Running() after StopAll = %v, want empty", got)
	}
}

// TestOrchestrator_StartOneThenStopAll covers the inverse shape: a channel
// started individually is torn down by StopAll, and channels never started are
// not stopped.
func TestOrchestrator_StartOneThenStopAll(t *testing.T) {
	orch, chans := newLifecycleOrchestrator("a", "b")

	if err := orch.StartChannel(context.Background(), "a"); err != nil {
		t.Fatalf("StartChannel(a) error: %v", err)
	}
	if got := orch.Running(); !equalStrings(got, []string{"a"}) {
		t.Fatalf("Running() = %v, want [a]", got)
	}
	if starts, _ := chans["b"].counts(); starts != 0 {
		t.Fatalf("channel b started %d times, want 0", starts)
	}

	orch.StopAll()
	if got := orch.Running(); len(got) != 0 {
		t.Errorf("Running() after StopAll = %v, want empty", got)
	}
	if _, stops := chans["a"].counts(); stops != 1 {
		t.Errorf("channel a Stop called %d times, want 1", stops)
	}
	if _, stops := chans["b"].counts(); stops != 0 {
		t.Errorf("never-started channel b Stop called %d times, want 0", stops)
	}
}

// TestOrchestrator_IdempotentStartStop verifies double start/stop are no-ops at
// both the orchestrator and channel level.
func TestOrchestrator_IdempotentStartStop(t *testing.T) {
	orch, chans := newLifecycleOrchestrator("a")

	for i := 0; i < 3; i++ {
		if err := orch.StartChannel(context.Background(), "a"); err != nil {
			t.Fatalf("StartChannel #%d error: %v", i+1, err)
		}
	}
	if starts, _ := chans["a"].counts(); starts != 1 {
		t.Fatalf("repeated StartChannel called channel Start %d times, want 1", starts)
	}

	if err := orch.StopChannel("a"); err != nil {
		t.Fatalf("StopChannel error: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := orch.StopChannel("a"); err != nil {
			t.Fatalf("repeated StopChannel #%d error: %v", i+1, err)
		}
	}
	if _, stops := chans["a"].counts(); stops != 1 {
		t.Fatalf("repeated StopChannel called channel Stop %d times, want 1", stops)
	}

	// StopAll is likewise a no-op when nothing is running.
	orch.StopAll()
	orch.StopAll()
	if _, stops := chans["a"].counts(); stops != 1 {
		t.Fatalf("StopAll after StopChannel called channel Stop %d times, want 1", stops)
	}
}

// TestOrchestrator_ConcurrentStartStop hammers start/stop from several
// goroutines to exercise the lifecycle mutex. Run with -race.
func TestOrchestrator_ConcurrentStartStop(t *testing.T) {
	orch, chans := newLifecycleOrchestrator("a", "b")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				_ = orch.StartChannel(context.Background(), "a")
				_ = orch.StartChannel(context.Background(), "b")
				return
			}
			_ = orch.StopChannel("a")
			_ = orch.StopChannel("b")
		}(i)
	}
	wg.Wait()
	orch.StopAll()

	// exactly one channel Start per successful start is not guaranteed under
	// racing stop/start (a stop may interleave), but state must converge and no
	// channel may be left running.
	if got := orch.Running(); len(got) != 0 {
		t.Fatalf("Running() = %v after StopAll, want empty", got)
	}
	for name, ch := range chans {
		_, stops := ch.counts()
		if stops == 0 {
			t.Errorf("channel %q was never stopped", name)
		}
	}
}

// TestOrchestrator_NoGoroutineLeak asserts that consumers exit after StopAll:
// the tracking channels never close Events(), so only ctx cancellation can end
// the consume loop.
func TestOrchestrator_NoGoroutineLeak(t *testing.T) {
	baseline := runtime.NumGoroutine()
	orch, _ := newLifecycleOrchestrator("a", "b", "c", "d")

	orch.StartAll(context.Background())
	if got := len(orch.Running()); got != 4 {
		t.Fatalf("expected 4 running channels, got %d", got)
	}

	orch.StopAll()

	const tolerance = 2
	deadline := time.Now().Add(5 * time.Second)
	for {
		n := runtime.NumGoroutine()
		if n <= baseline+tolerance {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine leak: baseline=%d, after StopAll=%d", baseline, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestOrchestrator_StartErrors is table-driven over the failure modes of
// starting a channel.
func TestOrchestrator_StartErrors(t *testing.T) {
	sentinel := errors.New("no credentials")
	cases := []struct {
		name        string
		registered  bool
		startErr    error
		wantErr     bool
		wantRunning bool
	}{
		{name: "unregistered channel", registered: false, wantErr: true},
		{name: "start failure propagates", registered: true, startErr: sentinel, wantErr: true},
		{name: "successful start", registered: true, wantErr: false, wantRunning: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orch := NewOrchestrator(quietLogger(), NewDispatcher(quietLogger(), nil))
			var ch *trackingChannel
			if tc.registered {
				ch = newTrackingChannel("a")
				ch.startErr = tc.startErr
				orch.Register("a", ch)
			}
			err := orch.StartChannel(context.Background(), "a")
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				if tc.startErr != nil && !errors.Is(err, sentinel) {
					t.Fatalf("error = %v, want %v", err, sentinel)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := orch.IsRunning("a"); got != tc.wantRunning {
				t.Errorf("IsRunning = %v, want %v", got, tc.wantRunning)
			}
			orch.StopAll()
		})
	}
}

// TestOrchestrator_StopUnregisteredIsNoOp documents the idempotent stop
// contract for a name the orchestrator has never seen.
func TestOrchestrator_StopUnregisteredIsNoOp(t *testing.T) {
	orch := NewOrchestrator(quietLogger(), NewDispatcher(quietLogger(), nil))
	if err := orch.StopChannel("ghost"); err != nil {
		t.Fatalf("StopChannel(ghost) error: %v", err)
	}
}

// TestOrchestrator_StartAllContinuesAfterFailure verifies one broken channel
// does not prevent the others from starting.
func TestOrchestrator_StartAllContinuesAfterFailure(t *testing.T) {
	orch, chans := newLifecycleOrchestrator("a", "b", "c")
	chans["b"].startErr = errors.New("boom")

	orch.StartAll(context.Background())
	if got := orch.Running(); !equalStrings(got, []string{"a", "c"}) {
		t.Errorf("Running() = %v, want [a c]", got)
	}
	orch.StopAll()
}

// TestOrchestrator_StartAfterStopAllRefused is the shutdown regression: a start
// that arrives after StopAll must be refused instead of resurrecting a channel
// whose consumer context has already been cancelled (and whose goroutine would
// then outlive the stopped orchestrator). It also pins StopAll's idempotence and
// the absence of a leaked consumer goroutine.
func TestOrchestrator_StartAfterStopAllRefused(t *testing.T) {
	orch, chans := newLifecycleOrchestrator("a")

	orch.StartAll(context.Background())
	if got := orch.Running(); !equalStrings(got, []string{"a"}) {
		t.Fatalf("Running() after StartAll = %v, want [a]", got)
	}

	orch.StopAll()
	orch.StopAll() // idempotent: must not stop the channel twice
	if _, stops := chans["a"].counts(); stops != 1 {
		t.Fatalf("channel Stop called %d times after two StopAll calls, want 1", stops)
	}

	baseline := runtime.NumGoroutine()
	for i := 0; i < 3; i++ {
		if err := orch.StartChannel(context.Background(), "a"); !errors.Is(err, ErrOrchestratorStopped) {
			t.Fatalf("StartChannel #%d after StopAll error = %v, want ErrOrchestratorStopped", i+1, err)
		}
	}
	orch.StartAll(context.Background()) // likewise refused, without an error return

	if got := orch.Running(); len(got) != 0 {
		t.Errorf("Running() after refused starts = %v, want empty", got)
	}
	if starts, _ := chans["a"].counts(); starts != 1 {
		t.Errorf("channel Start called %d times, want 1 (no resurrection)", starts)
	}

	// A refused start must not leave a fresh consumer goroutine behind.
	const tolerance = 2
	deadline := time.Now().Add(5 * time.Second)
	for {
		if n := runtime.NumGoroutine(); n <= baseline+tolerance {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine leak after refused start: baseline=%d, now=%d", baseline, runtime.NumGoroutine())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
