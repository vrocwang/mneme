package channels

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/simon/mneme/internal/config"
)

// fakeController records the runtime start/stop calls made by ChannelRPC.
type fakeController struct {
	mu       sync.Mutex
	running  map[string]bool
	starts   []string
	stops    []string
	startErr error
	stopErr  error
	// onStart runs before StartChannel returns, letting a test simulate a
	// concurrent config write landing between the enable save and the rollback.
	onStart func()
}

func newFakeController() *fakeController {
	return &fakeController{running: make(map[string]bool)}
}

func (f *fakeController) StartChannel(name string) error {
	f.mu.Lock()
	onStart := f.onStart
	f.mu.Unlock()
	if onStart != nil {
		onStart()
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return f.startErr
	}
	if f.running[name] {
		return nil // idempotent, like the real controller
	}
	f.running[name] = true
	f.starts = append(f.starts, name)
	return nil
}

func (f *fakeController) StopChannel(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopErr != nil {
		return f.stopErr
	}
	if !f.running[name] {
		return nil
	}
	delete(f.running, name)
	f.stops = append(f.stops, name)
	return nil
}

func (f *fakeController) Running() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, 0, len(f.running))
	for name := range f.running {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (f *fakeController) startedNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.starts))
	copy(out, f.starts)
	return out
}

func (f *fakeController) stoppedNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.stops))
	copy(out, f.stops)
	return out
}

// newTestRPC creates a ChannelRPC backed by a real config file in a temp dir so
// save/rollback behaviour is exercised against actual bytes.
func newTestRPC(t *testing.T, channels map[string]config.ChannelConfig, providers []string) (*ChannelRPC, *config.Config, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if channels == nil {
		channels = make(map[string]config.ChannelConfig)
	}
	cfg := &config.Config{Workspace: dir, Channels: channels}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("save initial config: %v", err)
	}
	rpc := NewChannelRPC(cfg, path, func() []string { return providers })
	return rpc, cfg, path
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func channelInfos(t *testing.T, res map[string]interface{}) map[string]map[string]interface{} {
	t.Helper()
	raw, ok := res["channels"].([]map[string]interface{})
	if !ok {
		t.Fatalf("channels payload has type %T, want []map[string]interface{}", res["channels"])
	}
	out := make(map[string]map[string]interface{}, len(raw))
	for _, entry := range raw {
		name, _ := entry["name"].(string)
		out[name] = entry
	}
	return out
}

// TestChannelRPC_EnableCreatesEntryAndStarts covers the "add a channel at
// runtime" path: a registry-provided name that has no config entry yet must get
// one (enabled) and be started immediately.
func TestChannelRPC_EnableCreatesEntryAndStarts(t *testing.T) {
	rpc, cfg, path := newTestRPC(t, nil, []string{"web", "cli", "telegram"})
	ctrl := newFakeController()
	rpc.SetController(ctrl)

	res := rpc.EnableChannel("telegram")
	if ok, _ := res["ok"].(bool); !ok {
		t.Fatalf("EnableChannel(telegram) failed: %v", res)
	}
	entry, exists := cfg.Channels["telegram"]
	if !exists {
		t.Fatal("config entry for telegram was not created")
	}
	if entry.Disabled {
		t.Error("created config entry is disabled, want enabled")
	}
	if got := ctrl.startedNames(); len(got) != 1 || got[0] != "telegram" {
		t.Errorf("controller starts = %v, want [telegram]", got)
	}
	if body := string(readFile(t, path)); !strings.Contains(body, "[channels.telegram]") {
		t.Errorf("saved config does not contain the new channel:\n%s", body)
	}
}

// TestChannelRPC_EnableAlreadyRunningIsNoOp verifies enabling a channel that is
// already running neither double-starts it nor errors.
func TestChannelRPC_EnableAlreadyRunningIsNoOp(t *testing.T) {
	rpc, _, path := newTestRPC(t, map[string]config.ChannelConfig{
		"web": {Disabled: false},
	}, []string{"web"})
	ctrl := newFakeController()
	// Simulate a channel started at boot.
	if err := ctrl.StartChannel("web"); err != nil {
		t.Fatalf("prime controller: %v", err)
	}
	ctrl.starts = nil
	rpc.SetController(ctrl)
	before := readFile(t, path)

	res := rpc.EnableChannel("web")
	if ok, _ := res["ok"].(bool); !ok {
		t.Fatalf("EnableChannel(web) failed: %v", res)
	}
	if got := ctrl.startedNames(); len(got) != 0 {
		t.Errorf("already-running channel started again: %v", got)
	}
	if after := readFile(t, path); !bytes.Equal(before, after) {
		t.Error("enabling an already-enabled channel rewrote the config")
	}
}

// TestChannelRPC_DisableStops verifies disable persists the flag and stops the
// running channel.
func TestChannelRPC_DisableStops(t *testing.T) {
	rpc, cfg, _ := newTestRPC(t, map[string]config.ChannelConfig{
		"web": {Disabled: false},
	}, []string{"web"})
	ctrl := newFakeController()
	ctrl.running["web"] = true
	rpc.SetController(ctrl)

	res := rpc.DisableChannel("web")
	if ok, _ := res["ok"].(bool); !ok {
		t.Fatalf("DisableChannel(web) failed: %v", res)
	}
	if !cfg.Channels["web"].Disabled {
		t.Error("config entry is not disabled after DisableChannel")
	}
	if got := ctrl.stoppedNames(); len(got) != 1 || got[0] != "web" {
		t.Errorf("controller stops = %v, want [web]", got)
	}
	if got := ctrl.Running(); len(got) != 0 {
		t.Errorf("controller still reports running: %v", got)
	}
}

// TestChannelRPC_DisableNotRunningSucceeds: the config is the source of truth
// for the next boot, so disabling a stopped channel still succeeds.
func TestChannelRPC_DisableNotRunningSucceeds(t *testing.T) {
	rpc, cfg, _ := newTestRPC(t, map[string]config.ChannelConfig{
		"cli": {Disabled: false},
	}, []string{"cli"})
	ctrl := newFakeController()
	rpc.SetController(ctrl)

	res := rpc.DisableChannel("cli")
	if ok, _ := res["ok"].(bool); !ok {
		t.Fatalf("DisableChannel(cli) failed: %v", res)
	}
	if !cfg.Channels["cli"].Disabled {
		t.Error("config entry is not disabled")
	}
	if got := ctrl.stoppedNames(); len(got) != 0 {
		t.Errorf("controller stops = %v, want none", got)
	}
}

// TestChannelRPC_DisableStopErrorKeepsDisabledFlag: a genuine runtime stop
// failure is reported, but the disabled flag stays because it governs the next
// boot.
func TestChannelRPC_DisableStopErrorKeepsDisabledFlag(t *testing.T) {
	rpc, cfg, _ := newTestRPC(t, map[string]config.ChannelConfig{
		"web": {Disabled: false},
	}, []string{"web"})
	ctrl := newFakeController()
	ctrl.running["web"] = true
	ctrl.stopErr = errors.New("websocket close failed")
	rpc.SetController(ctrl)

	res := rpc.DisableChannel("web")
	if ok, _ := res["ok"].(bool); ok {
		t.Fatalf("expected ok=false when stop fails, got %v", res)
	}
	if res["error"] == nil || res["error"] == "" {
		t.Errorf("expected an error message, got %v", res)
	}
	if !cfg.Channels["web"].Disabled {
		t.Error("disabled flag was dropped even though it drives the next boot")
	}
}

// TestChannelRPC_EnableRollsBackOnControllerError is the rollback regression: a
// controller failure must leave both memory and the config file exactly as they
// were for the channel entry (the file is rewritten by the same encoder, so the
// bytes are identical for an otherwise-unchanged config).
func TestChannelRPC_EnableRollsBackOnControllerError(t *testing.T) {
	cases := []struct {
		name     string
		channels map[string]config.ChannelConfig
		enable   string
		wantHad  bool
	}{
		{
			name:     "existing disabled entry",
			channels: map[string]config.ChannelConfig{"web": {Disabled: true}},
			enable:   "web",
			wantHad:  true,
		},
		{
			name:     "unconfigured registry channel",
			channels: map[string]config.ChannelConfig{"web": {Disabled: false}},
			enable:   "telegram",
			wantHad:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rpc, cfg, path := newTestRPC(t, tc.channels, []string{"web", "telegram"})
			ctrl := newFakeController()
			ctrl.startErr = errors.New("missing credentials")
			rpc.SetController(ctrl)
			before := readFile(t, path)

			res := rpc.EnableChannel(tc.enable)
			if ok, _ := res["ok"].(bool); ok {
				t.Fatalf("expected ok=false when the controller fails, got %v", res)
			}
			if res["error"] == nil || res["error"] == "" {
				t.Errorf("expected the controller error to be surfaced, got %v", res)
			}
			if after := readFile(t, path); !bytes.Equal(before, after) {
				t.Errorf("config was not rolled back byte-for-byte\nbefore:\n%s\nafter:\n%s", before, after)
			}
			if _, exists := cfg.Channels[tc.enable]; exists != tc.wantHad {
				t.Errorf("in-memory entry for %q exists=%v, want %v", tc.enable, exists, tc.wantHad)
			}
		})
	}
}

// TestChannelRPC_EnableRollbackPreservesOtherWrites is the lost-update
// regression for the enable rollback. A config write to an unrelated field that
// lands after the enable (simulated by the controller, which runs between the
// enable save and the rollback) must survive. The old code restored whole-file
// snapshot bytes and silently reverted it.
func TestChannelRPC_EnableRollbackPreservesOtherWrites(t *testing.T) {
	rpc, cfg, path := newTestRPC(t, map[string]config.ChannelConfig{
		"web": {Disabled: true},
	}, []string{"web"})

	ctrl := newFakeController()
	ctrl.onStart = func() {
		// A concurrent RPC updates an unrelated field through the shared config.
		cfg.Security.Tier = "full"
		if err := cfg.Save(path); err != nil {
			t.Errorf("concurrent config save: %v", err)
		}
	}
	ctrl.startErr = errors.New("missing credentials")
	rpc.SetController(ctrl)

	res := rpc.EnableChannel("web")
	if ok, _ := res["ok"].(bool); ok {
		t.Fatalf("expected ok=false when the controller fails, got %v", res)
	}

	if cfg.Security.Tier != "full" {
		t.Errorf("in-memory security.tier = %q, want full", cfg.Security.Tier)
	}
	body := string(readFile(t, path))
	if !strings.Contains(body, `tier = "full"`) {
		t.Errorf("the rollback reverted a concurrent write to security.tier:\n%s", body)
	}
	// The rollback's own guarantee still holds: the config must not claim an
	// enabled channel that could not start.
	if entry, ok := cfg.Channels["web"]; !ok || !entry.Disabled {
		t.Errorf("in-memory channel entry = %+v, want disabled", entry)
	}
	if !strings.Contains(body, "disabled = true") {
		t.Errorf("persisted channel entry is not disabled after rollback:\n%s", body)
	}
}

// TestChannelRPC_ListChannelsReportsRunning verifies the per-channel running
// flag the UI relies on, alongside the existing keys.
func TestChannelRPC_ListChannelsReportsRunning(t *testing.T) {
	rpc, _, _ := newTestRPC(t, map[string]config.ChannelConfig{
		"web": {Disabled: false},
		"cli": {Disabled: true},
	}, []string{"web", "cli"})
	ctrl := newFakeController()
	ctrl.running["web"] = true
	rpc.SetController(ctrl)

	res := rpc.ListChannels()
	if ok, _ := res["ok"].(bool); !ok {
		t.Fatalf("ListChannels ok=false: %v", res)
	}
	if _, ok := res["available"]; !ok {
		t.Error("ListChannels dropped the available key")
	}
	infos := channelInfos(t, res)
	if infos["web"] == nil || infos["cli"] == nil {
		t.Fatalf("ListChannels missing configured channels: %v", infos)
	}
	if running, _ := infos["web"]["running"].(bool); !running {
		t.Errorf("web running = %v, want true", infos["web"]["running"])
	}
	if running, _ := infos["cli"]["running"].(bool); running {
		t.Errorf("cli running = %v, want false", infos["cli"]["running"])
	}
	if enabled, _ := infos["cli"]["enabled"].(bool); enabled {
		t.Errorf("cli enabled = %v, want false", infos["cli"]["enabled"])
	}
}

// TestChannelRPC_UnknownChannelRejected makes sure a typo cannot silently add a
// bogus config entry.
func TestChannelRPC_UnknownChannelRejected(t *testing.T) {
	rpc, cfg, path := newTestRPC(t, nil, []string{"web", "cli"})
	ctrl := newFakeController()
	rpc.SetController(ctrl)
	before := readFile(t, path)

	for _, method := range []string{"enable", "disable"} {
		var res map[string]interface{}
		if method == "enable" {
			res = rpc.EnableChannel("bogus")
		} else {
			res = rpc.DisableChannel("bogus")
		}
		if ok, _ := res["ok"].(bool); ok {
			t.Errorf("%s(bogus) unexpectedly succeeded: %v", method, res)
		}
	}
	if len(cfg.Channels) != 0 {
		t.Errorf("bogus channel was added to config: %v", cfg.Channels)
	}
	if after := readFile(t, path); !bytes.Equal(before, after) {
		t.Error("rejected channel call rewrote the config")
	}
}

// TestChannelRPC_EnableWithoutControllerPersists: before wireRPC runs (or in a
// mode without runtime control) the config change is still saved.
func TestChannelRPC_EnableWithoutControllerPersists(t *testing.T) {
	rpc, cfg, path := newTestRPC(t, nil, []string{"web"})
	res := rpc.EnableChannel("web")
	if ok, _ := res["ok"].(bool); !ok {
		t.Fatalf("EnableChannel(web) failed without a controller: %v", res)
	}
	if entry, ok := cfg.Channels["web"]; !ok || entry.Disabled {
		t.Errorf("config entry = %+v, want enabled", entry)
	}
	if !strings.Contains(string(readFile(t, path)), "[channels.web]") {
		t.Error("config file does not contain the enabled channel")
	}
	// The running flag is simply false when no controller is wired.
	infos := channelInfos(t, rpc.ListChannels())
	if running, _ := infos["web"]["running"].(bool); running {
		t.Error("web reports running without a controller")
	}
}

// TestChannelRPC_EnableSaveFailureRollsBackMemory: when saving fails there is
// nothing new on disk to roll back, but memory must not diverge from the file.
func TestChannelRPC_EnableSaveFailureRollsBackMemory(t *testing.T) {
	rpc, cfg, path := newTestRPC(t, map[string]config.ChannelConfig{
		"web": {Disabled: true},
	}, []string{"web"})
	before := readFile(t, path)
	// Save writes atomically through "<path>.tmp"; occupying that name with a
	// directory makes the write fail deterministically while the real config
	// file stays readable.
	if err := os.Mkdir(path+".tmp", 0o755); err != nil {
		t.Fatalf("mkdir temp blocker: %v", err)
	}
	rpc.SetController(newFakeController())

	res := rpc.EnableChannel("web")
	if ok, _ := res["ok"].(bool); ok {
		t.Fatalf("expected ok=false when saving fails, got %v", res)
	}
	if !cfg.Channels["web"].Disabled {
		t.Error("in-memory entry was not restored after a failed save")
	}
	if after := readFile(t, path); !bytes.Equal(before, after) {
		t.Error("config file changed despite a failed save")
	}
}
