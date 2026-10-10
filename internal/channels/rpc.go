package channels

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"sort"

	"github.com/simon/mneme/internal/config"
)

// ChannelController applies a channel enable/disable to the running process.
// It is defined here (rather than in boot, which owns the orchestrator) to
// keep the import direction one-way: capability and boot import channels, so
// channels must not import them. boot.ChannelController implements it with a
// lazily-resolved orchestrator, because the GUI orchestrator is created during
// startup while the RPC object is constructed before it.
type ChannelController interface {
	// StartChannel starts the named channel. It is idempotent: a channel that
	// is already running is left untouched and nil is returned.
	StartChannel(name string) error
	// StopChannel stops the named channel. Stopping a channel that is not
	// running succeeds.
	StopChannel(name string) error
	// Running returns the names of the channels currently running.
	Running() []string
}

// ChannelRPC provides Wails-bound methods for the frontend channels page.
type ChannelRPC struct {
	cfg      *config.Config
	confPath string
	// providerNames returns the channel providers currently registered in the
	// capability registry. It is injected (rather than imported) because the
	// capability package already imports channels; a direct import would cycle.
	providerNames func() []string
	// controller applies start/stop to the live process. It is injected after
	// startup (the orchestrator does not exist before AppCore.Init) and may be
	// nil, in which case the config is still updated and takes effect on the
	// next boot.
	controller ChannelController
}

// NewChannelRPC creates a Wails RPC handler for channel management.
// providerNames may be nil; the two core channel types are reported as a
// fallback in that case.
func NewChannelRPC(cfg *config.Config, confPath string, providerNames func() []string) *ChannelRPC {
	return &ChannelRPC{cfg: cfg, confPath: confPath, providerNames: providerNames}
}

// SetController injects the runtime channel controller. It is called from
// wireRPC after AppCore.Init, mirroring CapabilityRPC.SetRegistry.
func (r *ChannelRPC) SetController(c ChannelController) { r.controller = c }

// ListChannels returns configured channels (with their live running state) and
// the available provider names.
func (r *ChannelRPC) ListChannels() map[string]interface{} {
	running := make(map[string]bool)
	if r.controller != nil {
		for _, name := range r.controller.Running() {
			running[name] = true
		}
	}
	list := make([]map[string]interface{}, 0)
	if r.cfg != nil {
		for name, c := range r.cfg.Channels {
			list = append(list, map[string]interface{}{
				"name":    name,
				"enabled": !c.Disabled,
				"type":    name,
				"running": running[name],
			})
		}
	}
	// Deterministic order: the frontend renders this list directly and map
	// iteration order would reshuffle it on every poll.
	sort.Slice(list, func(i, j int) bool {
		return list[i]["name"].(string) < list[j]["name"].(string)
	})
	return map[string]interface{}{
		"ok":        true,
		"channels":  list,
		"available": r.availableChannelTypes(),
	}
}

// EnableChannel enables a channel: it accepts any name offered by the registry
// (creating the config entry when it does not exist yet), persists the change,
// then starts the channel in the running process. If the runtime start fails
// (e.g. missing credentials) the persisted change is rolled back so the config
// never claims a channel is on that could not actually start.
func (r *ChannelRPC) EnableChannel(name string) map[string]interface{} {
	if r.cfg == nil {
		return map[string]interface{}{"ok": false, "error": "config not available"}
	}
	if !r.knownChannel(name) {
		return map[string]interface{}{"ok": false, "error": "channel not found: " + name}
	}

	before, existed, err := r.snapshotConfigFile()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	prev, hadPrev := r.cfg.Channels[name]

	ch := prev // zero value when the channel was not configured yet
	ch.Disabled = false
	r.cfg.Channels[name] = ch
	if err := r.cfg.Save(r.confPath); err != nil {
		r.rollbackChannel(name, prev, hadPrev, before, existed)
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}

	if r.controller != nil {
		if err := r.controller.StartChannel(name); err != nil {
			r.rollbackChannel(name, prev, hadPrev, before, existed)
			return map[string]interface{}{"ok": false, "error": err.Error()}
		}
	}
	return map[string]interface{}{"ok": true, "name": name, "enabled": true}
}

// DisableChannel disables a channel in config and stops it at runtime. It
// succeeds when the channel is not running: the config is the source of truth
// on the next boot. A genuine runtime stop failure is reported, but the
// disabled flag is kept because it is what will be honoured at the next start.
func (r *ChannelRPC) DisableChannel(name string) map[string]interface{} {
	if r.cfg == nil {
		return map[string]interface{}{"ok": false, "error": "config not available"}
	}
	if !r.knownChannel(name) {
		return map[string]interface{}{"ok": false, "error": "channel not found: " + name}
	}

	prev, hadPrev := r.cfg.Channels[name]
	ch := prev
	ch.Disabled = true
	r.cfg.Channels[name] = ch
	if err := r.cfg.Save(r.confPath); err != nil {
		// Persisting failed, so keep memory consistent with the file on disk
		// rather than reporting a state change that will not survive a restart.
		if hadPrev {
			r.cfg.Channels[name] = prev
		} else {
			delete(r.cfg.Channels, name)
		}
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}

	if r.controller != nil {
		if err := r.controller.StopChannel(name); err != nil {
			return map[string]interface{}{
				"ok": false, "name": name, "enabled": false,
				"error": "channel disabled in config but failed to stop: " + err.Error(),
			}
		}
	}
	return map[string]interface{}{"ok": true, "name": name, "enabled": false}
}

// knownChannel reports whether name is a configured channel or one the
// registry can provide. Names that are neither are rejected so a typo cannot
// silently add a bogus config entry.
func (r *ChannelRPC) knownChannel(name string) bool {
	if _, ok := r.cfg.Channels[name]; ok {
		return true
	}
	for _, n := range r.availableChannelTypes() {
		if n == name {
			return true
		}
	}
	return false
}

// availableChannelTypes returns the channel provider names registered in the
// capability registry, falling back to the two core types when the registry is
// not reachable yet (e.g. before startup).
func (r *ChannelRPC) availableChannelTypes() []string {
	if r.providerNames != nil {
		if names := r.providerNames(); len(names) > 0 {
			return names
		}
	}
	return []string{ChannelWeb, ChannelCLI}
}

// snapshotConfigFile reads the current config file so a failed enable can be
// rolled back byte-for-byte. existed reports whether the file was present at
// all (a missing file means rollback deletes it again).
func (r *ChannelRPC) snapshotConfigFile() (data []byte, existed bool, err error) {
	if r.confPath == "" {
		return nil, false, errors.New("config path not set")
	}
	data, err = os.ReadFile(r.confPath)
	if err == nil {
		return data, true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	return nil, false, err
}

// rollbackChannel restores one channel's in-memory entry, re-saves the config,
// and finally restores the exact previous file bytes so the rollback is
// byte-for-byte even if the TOML encoder is not order-stable.
func (r *ChannelRPC) rollbackChannel(name string, prev config.ChannelConfig, hadPrev bool, before []byte, existed bool) {
	if hadPrev {
		r.cfg.Channels[name] = prev
	} else {
		delete(r.cfg.Channels, name)
	}
	_ = r.cfg.Save(r.confPath) // best effort; the byte restore below is authoritative

	if existed {
		if cur, err := os.ReadFile(r.confPath); err == nil && !bytes.Equal(cur, before) {
			_ = os.WriteFile(r.confPath, before, 0o600)
		}
		return
	}
	_ = os.Remove(r.confPath)
}
