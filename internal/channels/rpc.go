package channels

import "github.com/simon/mneme/internal/config"

// ChannelRPC provides Wails-bound methods for the frontend channels page.
type ChannelRPC struct {
	cfg      *config.Config
	confPath string
}

// NewChannelRPC creates a Wails RPC handler for channel management.
func NewChannelRPC(cfg *config.Config, confPath string) *ChannelRPC {
	return &ChannelRPC{cfg: cfg, confPath: confPath}
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
			list = append(list, chInfo{Name: name, Enabled: c.Enabled, Type: name})
		}
	}
	return map[string]interface{}{
		"ok":        true,
		"channels":  list,
		"available": availableChannelTypes(),
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
	ch.Enabled = true
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
	ch.Enabled = false
	r.cfg.Channels[name] = ch
	if err := r.cfg.Save(r.confPath); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "name": name, "enabled": false}
}

// availableChannelTypes returns the known channel type names.
// Extensions can register additional types via capability.RegisterChannel.
func availableChannelTypes() []string {
	// Core types always available
	return []string{"web", "cli"}
}
