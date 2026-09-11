package capability

import (
	"fmt"
	"sync"

	"github.com/simon/mneme/internal/channels"
)

// ChannelProvider creates a Channel from configuration.
// Extensions register providers to add channel types without modifying core code.
type ChannelProvider interface {
	// Name returns the channel type name (e.g. "telegram", "discord").
	Name() string
	// Create builds a Channel from the given config.
	Create(cfg ChannelConfig) (Channel, error)
}

// ChannelConfig is a generic config for channel initialization. Token and
// WebhookURL cover the common credential shapes; Extras carries anything
// provider-specific.
type ChannelConfig struct {
	Token      string
	WebhookURL string
	Extras     map[string]string
}

// Channel is the seam Definition for messaging channels, shared with the
// channels package (which owns the interface). Aliasing here keeps the
// capability registry free of a duplicated interface: there is exactly one
// Definition, and in-process (web/cli) and future process-isolated providers
// both implement channels.Channel.
type Channel = channels.Channel

// ── Channel registration in CapabilityRegistry ──────────────────────────

// channelEntry holds a registered channel provider and its created instance.
// The per-entry mutex serializes first-use creation without holding the
// registry lock across provider code (a provider's Create may call back into
// the registry, which would deadlock on the non-reentrant registry mutex).
type channelEntry struct {
	mu       sync.Mutex
	provider ChannelProvider
	instance Channel
	setID    string
}

// RegisterChannel adds a channel provider to the registry under the given set.
func (r *CapabilityRegistry) RegisterChannel(setID string, provider ChannelProvider) error {
	if provider == nil {
		return fmt.Errorf("nil channel provider")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.channels[provider.Name()]; exists {
		return fmt.Errorf("channel provider %q already registered", provider.Name())
	}
	r.channels[provider.Name()] = &channelEntry{provider: provider, setID: setID}
	return nil
}

// GetChannel returns the channel instance for the given name and config,
// creating and caching it on first use. Repeated calls with the same name
// return the same instance so the orchestrator never double-starts a channel.
func (r *CapabilityRegistry) GetChannel(name string, cfg ChannelConfig) (Channel, error) {
	r.mu.RLock()
	entry, ok := r.channels[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("channel %q not registered", name)
	}

	// Serialize creation per entry; the registry lock is NOT held here.
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.instance != nil {
		return entry.instance, nil
	}

	ch, err := entry.provider.Create(cfg)
	if err != nil {
		return nil, fmt.Errorf("create channel %q: %w", name, err)
	}
	entry.instance = ch
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

// ── Built-in channel helpers ────────────────────────────────────────────

// RegisterBuiltinChannel registers a core channel provider under the
// "builtin:channels" capability set and keeps that set's ChannelCount in sync
// with the providers it actually owns. Used for web and cli channels.
func (r *CapabilityRegistry) RegisterBuiltinChannel(provider ChannelProvider) error {
	if err := r.RegisterChannel(ChannelsSetID, provider); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	set := r.ensureSetLocked(ChannelsSetID, "Built-in Channels", KindBuiltin)
	count := 0
	for _, entry := range r.channels {
		if entry.setID == ChannelsSetID {
			count++
		}
	}
	set.ChannelCount = count
	return nil
}

// ChanProviderFunc adapts a function to the ChannelProvider interface.
type ChanProviderFunc struct {
	NameStr  string
	CreateFn func(ChannelConfig) (Channel, error)
}

func (p *ChanProviderFunc) Name() string { return p.NameStr }
func (p *ChanProviderFunc) Create(cfg ChannelConfig) (Channel, error) {
	return p.CreateFn(cfg)
}
