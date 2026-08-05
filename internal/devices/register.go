package devices

import (
	"log/slog"

	"github.com/simon/mneme/internal/capability"
)

// DevicesRPC provides Wails-bound methods for mobile device pairing.
type DevicesRPC struct {
	mgr *Manager
}

// NewDevicesRPC creates a Wails RPC handler backed by a device manager.
func NewDevicesRPC(mgr *Manager) *DevicesRPC {
	return &DevicesRPC{mgr: mgr}
}

// RegisterRPC implements capability.WailsRPCRegistrar.
func (r *DevicesRPC) RegisterRPC() []interface{} { return []interface{}{r} }

// GenerateCode generates a new pairing code.
func (r *DevicesRPC) GenerateCode() *PairingCode {
	return r.mgr.GeneratePairingCode()
}

// ValidateCode validates a pairing code string.
func (r *DevicesRPC) ValidateCode(code string) (*PairingCode, error) {
	return r.mgr.ValidatePairingCode(code)
}

// ListDevices returns all registered devices.
func (r *DevicesRPC) ListDevices() []Device {
	return r.mgr.ListDevices()
}

// ListPaired returns paired devices.
func (r *DevicesRPC) ListPaired() []Device {
	return r.mgr.ListPaired()
}

// Unpair removes a paired device by ID.
func (r *DevicesRPC) Unpair(deviceID string) {
	r.mgr.UnpairDevice(deviceID)
}

// ActiveTunnelCount returns how many tunnels are currently open.
func (r *DevicesRPC) ActiveTunnelCount() int {
	return r.mgr.ActiveTunnelCount()
}

// Register registers devices RPC bindings.
func Register(log *slog.Logger) {
	capability.RegisterWailsRPC(NewDevicesRPC(NewManager(log)))
}
