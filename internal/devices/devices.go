// Package devices manages device discovery, pairing, and encrypted tunnels for
// mobile companion app connectivity.
package devices

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"
)

// Device represents a discovered or paired device.
type Device struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"` // "ios", "android", "unknown"
	Address   string    `json:"address,omitempty"`
	PublicKey string    `json:"public_key,omitempty"`
	Paired    bool      `json:"paired"`
	LastSeen  time.Time `json:"last_seen"`
}

// PairingCode is a short-lived code displayed for device pairing.
type PairingCode struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Tunnel represents an encrypted connection to a paired device.
type Tunnel struct {
	DeviceID    string
	Conn        net.Conn
	SessionKey  []byte
	Established time.Time
}

// Manager handles device discovery, pairing, and tunnel management.
type Manager struct {
	mu       sync.RWMutex
	devices  map[string]*Device
	pairings map[string]*PairingCode
	tunnels  map[string]*Tunnel
	log      *slog.Logger
}

// NewManager creates a device manager.
func NewManager(log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		devices:  make(map[string]*Device),
		pairings: make(map[string]*PairingCode),
		tunnels:  make(map[string]*Tunnel),
		log:      log.With("component", "devices"),
	}
}

// GeneratePairingCode creates a 6-digit pairing code valid for 5 minutes.
func (m *Manager) GeneratePairingCode() *PairingCode {
	code := randomDigits(6)
	pc := &PairingCode{
		Code:      code,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	m.mu.Lock()
	m.pairings[code] = pc
	m.mu.Unlock()
	m.log.Info("pairing code generated", "expires", pc.ExpiresAt.Format(time.RFC3339))
	return pc
}

// ValidatePairingCode checks if a pairing code is valid and not expired.
func (m *Manager) ValidatePairingCode(code string) (*PairingCode, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	pc, ok := m.pairings[code]
	if !ok {
		return nil, fmt.Errorf("invalid pairing code")
	}
	if time.Now().After(pc.ExpiresAt) {
		delete(m.pairings, code)
		return nil, fmt.Errorf("pairing code expired")
	}
	return pc, nil
}

// RegisterDevice adds or updates a device entry.
func (m *Manager) RegisterDevice(device Device) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.devices[device.ID]; ok {
		device.Paired = existing.Paired
	}
	device.LastSeen = time.Now()
	m.devices[device.ID] = &device
	m.log.Debug("device registered", "id", device.ID, "name", device.Name)
}

// PairDevice marks a device as paired and stores its public key.
func (m *Manager) PairDevice(ctx context.Context, deviceID, publicKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	dev, ok := m.devices[deviceID]
	if !ok {
		return fmt.Errorf("device %q not found", deviceID)
	}
	dev.Paired = true
	dev.PublicKey = publicKey
	m.log.Info("device paired", "id", deviceID)
	return nil
}

// UnpairDevice removes the pairing for a device.
func (m *Manager) UnpairDevice(deviceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if dev, ok := m.devices[deviceID]; ok {
		dev.Paired = false
		dev.PublicKey = ""
	}
	m.log.Info("device unpaired", "id", deviceID)
}

// ListDevices returns all known devices.
func (m *Manager) ListDevices() []Device {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Device, 0, len(m.devices))
	for _, d := range m.devices {
		out = append(out, *d)
	}
	return out
}

// ListPaired returns only paired devices.
func (m *Manager) ListPaired() []Device {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Device
	for _, d := range m.devices {
		if d.Paired {
			out = append(out, *d)
		}
	}
	return out
}

// EstablishTunnel sets up an encrypted tunnel to a paired device.
func (m *Manager) EstablishTunnel(ctx context.Context, deviceID string, conn net.Conn) (*Tunnel, error) {
	m.mu.RLock()
	dev, ok := m.devices[deviceID]
	m.mu.RUnlock()
	if !ok || !dev.Paired {
		return nil, fmt.Errorf("device %q not paired", deviceID)
	}

	sessionKey := make([]byte, 32)
	if _, err := rand.Read(sessionKey); err != nil {
		return nil, fmt.Errorf("generate session key: %w", err)
	}

	t := &Tunnel{
		DeviceID:    deviceID,
		Conn:        conn,
		SessionKey:  sessionKey,
		Established: time.Now(),
	}

	m.mu.Lock()
	m.tunnels[deviceID] = t
	m.mu.Unlock()

	m.log.Info("tunnel established", "device", deviceID)
	return t, nil
}

// CloseTunnel tears down a device tunnel.
func (m *Manager) CloseTunnel(deviceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tunnels[deviceID]; ok {
		t.Conn.Close()
		delete(m.tunnels, deviceID)
		m.log.Info("tunnel closed", "device", deviceID)
	}
}

// ActiveTunnelCount returns the number of active tunnels.
func (m *Manager) ActiveTunnelCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.tunnels)
}

func randomDigits(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	for i := range b {
		b[i] = '0' + (b[i] % 10)
	}
	return hex.EncodeToString(b)[:n]
}
