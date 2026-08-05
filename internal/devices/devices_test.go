package devices

import (
	"context"
	"testing"
	"time"
)

func TestNewManager(t *testing.T) {
	m := NewManager(nil)
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestGeneratePairingCode(t *testing.T) {
	m := NewManager(nil)
	pc := m.GeneratePairingCode()

	if len(pc.Code) != 6 {
		t.Fatalf("expected 6-digit code, got %s", pc.Code)
	}
	if time.Now().After(pc.ExpiresAt) {
		t.Fatal("pairing code already expired")
	}
}

func TestValidatePairingCode(t *testing.T) {
	m := NewManager(nil)
	pc := m.GeneratePairingCode()

	valid, err := m.ValidatePairingCode(pc.Code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid.Code != pc.Code {
		t.Fatal("code mismatch")
	}
}

func TestValidatePairingCodeInvalid(t *testing.T) {
	m := NewManager(nil)
	_, err := m.ValidatePairingCode("000000")
	if err == nil {
		t.Fatal("expected error for invalid code")
	}
}

func TestDeviceRegistration(t *testing.T) {
	m := NewManager(nil)

	m.RegisterDevice(Device{ID: "dev1", Name: "Phone", Kind: "ios"})
	m.RegisterDevice(Device{ID: "dev2", Name: "Tablet", Kind: "android"})

	devices := m.ListDevices()
	if len(devices) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(devices))
	}

	paired := m.ListPaired()
	if len(paired) != 0 {
		t.Fatalf("expected 0 paired, got %d", len(paired))
	}
}

func TestPairUnpairDevice(t *testing.T) {
	m := NewManager(nil)
	m.RegisterDevice(Device{ID: "dev1", Name: "Phone", Kind: "ios"})

	if err := m.PairDevice(context.Background(), "dev1", "pubkey123"); err != nil {
		t.Fatalf("PairDevice: %v", err)
	}

	paired := m.ListPaired()
	if len(paired) != 1 {
		t.Fatalf("expected 1 paired, got %d", len(paired))
	}
	if paired[0].PublicKey != "pubkey123" {
		t.Fatalf("expected pubkey123, got %s", paired[0].PublicKey)
	}

	m.UnpairDevice("dev1")
	if len(m.ListPaired()) != 0 {
		t.Fatal("expected 0 paired after unpaired")
	}
}

func TestActiveTunnelCount(t *testing.T) {
	m := NewManager(nil)
	if m.ActiveTunnelCount() != 0 {
		t.Fatal("expected 0 tunnels initially")
	}
}
