package capability

import (
	"fmt"

	"github.com/simon/mneme/internal/channels"
)

// ChannelProvider creates a Channel from configuration.
// Extensions register providers to add channel types without modifying core code.
type ChannelProvider interface {
	// Name returns the channel type name (e.g. "telegram", "discord").
	Name() string
	// Create builds a Channel from the given config map.
	Create(cfg ChannelConfig) (Channel, error)
	// SupportsConfig returns true if the given config is valid for this provider.
	SupportsConfig(cfg ChannelConfig) bool
}

// ChannelConfig is a generic key-value config for channel initialization.
// Mirrors config.ChannelConfig but avoids circular imports.
type ChannelConfig struct {
	Token         string
	SigningSecret string
	WebhookURL    string
	PhoneNumberID string
	Extras        map[string]string
}

// Channel is the seam Definition for messaging channels, shared with the
// channels package (which owns the interface). Aliasing here keeps the
// capability registry free of a duplicated interface: there is exactly one
// Definition, and in-process (web/cli) and future process-isolated providers
// both implement channels.Channel.
type Channel = channels.Channel

// ChannelMessage is the message type flowing through a channel. Aliased to the
// single channels.Message so no conversion adapters are needed between the
// registry and the orchestrator.
type ChannelMessage = channels.Message

// ── Channel registration in CapabilityRegistry ──────────────────────────

// channelEntry holds a registered channel provider and its lifecycle state.
type channelEntry struct {
	provider ChannelProvider
	instance Channel
	config   ChannelConfig
	setID    string
	running  bool
}

// RegisterChannel adds a channel provider to the registry under the given set.
func (r *CapabilityRegistry) RegisterChannel(setID string, provider ChannelProvider) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.channels[provider.Name()]; exists {
		return fmt.Errorf("channel provider %q already registered", provider.Name())
	}
	if r.channels == nil {
		r.channels = make(map[string]*channelEntry)
	}
	r.channels[provider.Name()] = &channelEntry{provider: provider, setID: setID}
	return nil
}

// UnregisterChannel removes a channel provider and stops its instance.
func (r *CapabilityRegistry) UnregisterChannel(name string) error {
	r.mu.Lock()
	entry, ok := r.channels[name]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("channel %q not found", name)
	}
	if entry.instance != nil {
		entry.instance.Stop()
	}
	r.mu.Lock()
	delete(r.channels, name)
	r.mu.Unlock()
	return nil
}

// GetChannel returns a running channel instance for the given name and config.
// If the channel is not yet started, it creates and starts it.
func (r *CapabilityRegistry) GetChannel(name string, cfg ChannelConfig) (Channel, error) {
	r.mu.Lock()
	entry, ok := r.channels[name]
	r.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("channel %q not registered", name)
	}

	if entry.instance != nil && entry.running {
		return entry.instance, nil
	}

	ch, err := entry.provider.Create(cfg)
	if err != nil {
		return nil, fmt.Errorf("create channel %q: %w", name, err)
	}

	r.mu.Lock()
	entry.instance = ch
	entry.config = cfg
	r.mu.Unlock()

	return ch, nil
}

// ListChannels returns all registered channel provider names.
func (r *CapabilityRegistry) ListChannels() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.channels))
	for n := range r.channels {
		names = append(names, n)
	}
	return names
}

// GetChannelConfig returns the stored config for a channel, or empty.
func (r *CapabilityRegistry) GetChannelConfig(name string) ChannelConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if entry, ok := r.channels[name]; ok {
		return entry.config
	}
	return ChannelConfig{}
}

// ── Channel set management ──────────────────────────────────────────────

// UpdateSetChannelCount syncs the channel count in the set descriptor.
func (r *CapabilityRegistry) UpdateSetChannelCount(setID string, count int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.sets[setID]; ok {
		s.ChannelCount = count
	}
}

// ── Built-in channel helpers ────────────────────────────────────────────

// RegisterBuiltinChannel registers a core channel provider under the
// "builtin:channels" capability set. Used for web and cli channels.
func (r *CapabilityRegistry) RegisterBuiltinChannel(provider ChannelProvider) error {
	const builtinSet = "builtin:channels"
	r.mu.Lock()
	if _, exists := r.sets[builtinSet]; !exists {
		r.sets[builtinSet] = &CapabilitySet{
			ID:      builtinSet,
			Name:    "Built-in Channels",
			Kind:    KindBuiltin,
			Health:  HealthOK,
			Enabled: true,
		}
	}
	r.mu.Unlock()

	if err := r.RegisterChannel(builtinSet, provider); err != nil {
		return err
	}
	r.UpdateSetChannelCount(builtinSet, len(r.ListChannels()))
	return nil
}

// ChanProviderFunc adapts a function to the ChannelProvider interface.
type ChanProviderFunc struct {
	NameStr    string
	CreateFn   func(ChannelConfig) (Channel, error)
	SupportsFn func(ChannelConfig) bool
}

func (p *ChanProviderFunc) Name() string                              { return p.NameStr }
func (p *ChanProviderFunc) Create(cfg ChannelConfig) (Channel, error) { return p.CreateFn(cfg) }
func (p *ChanProviderFunc) SupportsConfig(cfg ChannelConfig) bool {
	if p.SupportsFn != nil {
		return p.SupportsFn(cfg)
	}
	return true
}
