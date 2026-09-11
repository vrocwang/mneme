package channels

import "github.com/simon/mneme/internal/config"

// ChannelRPC provides Wails-bound methods for the frontend channels page.
type ChannelRPC struct {
	cfg      *config.Config
	confPath string
	// providerNames returns the channel providers currently registered in the
	// capability registry. It is injected (rather than imported) because the
	// capability package already imports channels; a direct import would cycle.
	providerNames func() []string
}

// NewChannelRPC creates a Wails RPC handler for channel management.
// providerNames may be nil; the two core channel types are reported as a
// fallback in that case.
func NewChannelRPC(cfg *config.Config, confPath string, providerNames func() []string) *ChannelRPC {
	return &ChannelRPC{cfg: cfg, confPath: confPath, providerNames: providerNames}
}

// ListChannels returns configured channels and available provider names.
func (r *ChannelRPC) ListChannels() map[string]interface{} {
	type chInfo struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
		Type    string `json:"type"`
	}
	var list []chInfo
	if r.cfg != nil {
		for name, c := range r.cfg.Channels {
			list = append(list, chInfo{Name: name, Enabled: !c.Disabled, Type: name})
		}
	}
	return map[string]interface{}{
		"ok":        true,
		"channels":  list,
		"available": r.availableChannelTypes(),
	}
}

// EnableChannel sets a channel's enabled flag to true and saves the config.
func (r *ChannelRPC) EnableChannel(name string) map[string]interface{} {
	if r.cfg == nil {
		return map[string]interface{}{"ok": false, "error": "config not available"}
	}
	ch, ok := r.cfg.Channels[name]
	if !ok {
		return map[string]interface{}{"ok": false, "error": "channel not found: " + name}
	}
	ch.Disabled = false
	r.cfg.Channels[name] = ch
	if err := r.cfg.Save(r.confPath); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "name": name, "enabled": true}
}

// DisableChannel sets a channel's enabled flag to false and saves the config.
func (r *ChannelRPC) DisableChannel(name string) map[string]interface{} {
	if r.cfg == nil {
		return map[string]interface{}{"ok": false, "error": "config not available"}
	}
	ch, ok := r.cfg.Channels[name]
	if !ok {
		return map[string]interface{}{"ok": false, "error": "channel not found: " + name}
	}
	ch.Disabled = true
	r.cfg.Channels[name] = ch
	if err := r.cfg.Save(r.confPath); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "name": name, "enabled": false}
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
	return []string{"web", "cli"}
}
