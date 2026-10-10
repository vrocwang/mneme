package boot

import (
	"context"
	"errors"
	"fmt"

	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/channels"
	"github.com/simon/mneme/internal/config"
)

// ErrChannelsUnavailable is returned by ChannelController in modes that do not
// run channels (headless CLI/run/MCP). The RPC surfaces it verbatim so the UI
// reports a reason instead of a silent failure.
var ErrChannelsUnavailable = errors.New("channels are not available in this mode")

// ChannelController implements channels.ChannelController against a live
// AppCore. Both the orchestrator and the registry are resolved lazily on every
// call: the Wails RPC object is constructed before AppCore.Init runs, and the
// orchestrator only exists in the non-headless branch of Init, so capturing
// either at construction time would freeze a nil.
type ChannelController struct {
	core *AppCore
}

// NewChannelController returns a controller bound to core.
func NewChannelController(core *AppCore) *ChannelController {
	return &ChannelController{core: core}
}

// StartChannel resolves the channel instance from the capability registry,
// registers it with the orchestrator when new, and starts it. Starting an
// already-running channel is a no-op.
func (c *ChannelController) StartChannel(name string) error {
	orch, err := c.orchestrator()
	if err != nil {
		return err
	}
	if orch.IsRunning(name) {
		return nil
	}
	ch, err := c.channelFor(name)
	if err != nil {
		return err
	}
	// GetChannel caches instances, so registering again would be harmless, but
	// only register when absent to keep the orchestrator's map authoritative.
	if !orch.IsRunning(name) {
		orch.Register(name, ch)
	}
	return orch.StartChannel(context.Background(), name)
}

// StopChannel stops the channel at runtime. Stopping a channel that is not
// running succeeds (the disabled config flag governs the next boot).
func (c *ChannelController) StopChannel(name string) error {
	orch, err := c.orchestrator()
	if err != nil {
		return err
	}
	return orch.StopChannel(name)
}

// Running returns the names of the running channels, or nil in modes without
// an orchestrator.
func (c *ChannelController) Running() []string {
	orch, err := c.orchestrator()
	if err != nil {
		return nil
	}
	return orch.Running()
}

func (c *ChannelController) orchestrator() (*channels.Orchestrator, error) {
	if c == nil || c.core == nil || c.core.ChannelOrch == nil {
		return nil, ErrChannelsUnavailable
	}
	return c.core.ChannelOrch, nil
}

// channelFor builds the channel's capability config from config.toml and asks
// the registry for the (cached) instance.
func (c *ChannelController) channelFor(name string) (channels.Channel, error) {
	if c.core.CapReg == nil {
		return nil, fmt.Errorf("channel %q: capability registry not available", name)
	}
	var chCfg config.ChannelConfig
	if c.core.Cfg != nil {
		chCfg = c.core.Cfg.Channels[name]
	}
	ch, err := c.core.CapReg.GetChannel(name, capability.ChannelConfig{
		Token:      chCfg.Token,
		WebhookURL: chCfg.WebhookURL,
		Extras:     chCfg.Extra,
	})
	if err != nil {
		return nil, err
	}
	return ch, nil
}
