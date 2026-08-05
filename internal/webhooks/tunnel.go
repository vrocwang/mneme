package webhooks

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/simon/mneme/pkg/events"
)

type TunnelTarget string

const (
	TargetEcho  TunnelTarget = "echo"
	TargetAgent TunnelTarget = "agent"
	TargetSkill TunnelTarget = "skill"
)

type TunnelRegistration struct {
	ID          string       `json:"id"`
	TunnelUUID  string       `json:"tunnel_uuid"`
	Target      TunnelTarget `json:"target"`
	TargetID    string       `json:"target_id"`
	Description string       `json:"description,omitempty"`
	Enabled     bool         `json:"enabled"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

type WebhookActivityEntry struct {
	ID           string    `json:"id"`
	TunnelUUID   string    `json:"tunnel_uuid"`
	RequestID    string    `json:"request_id"`
	Status       int       `json:"status"`
	ResponseSize int       `json:"response_size"`
	Duration     int64     `json:"duration_ms"`
	Error        string    `json:"error,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type TunnelManager struct {
	mu            sync.RWMutex
	registrations map[string]*TunnelRegistration
	activities    []WebhookActivityEntry
	maxActivities int
	persistPath   string
	bandwidth     map[string]int64
	eventBus      *events.Bus
}

func (tm *TunnelManager) SetEventBus(bus *events.Bus) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.eventBus = bus
}

func NewTunnelManager(persistPath string) *TunnelManager {
	tm := &TunnelManager{
		registrations: make(map[string]*TunnelRegistration),
		activities:    make([]WebhookActivityEntry, 0, 100),
		maxActivities: 200,
		persistPath:   persistPath,
		bandwidth:     make(map[string]int64),
	}
	if persistPath != "" {
		tm.load()
	}
	return tm
}

func (tm *TunnelManager) RegisterTunnel(target TunnelTarget, targetID, description string) (*TunnelRegistration, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.ensureInit()

	reg := &TunnelRegistration{
		ID:          uuid.New().String(),
		TunnelUUID:  uuid.New().String(),
		Target:      target,
		TargetID:    targetID,
		Description: description,
		Enabled:     true,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	tm.registrations[reg.TunnelUUID] = reg
	tm.persist()
	tm.publishRegistered(reg)
	return reg, nil
}

func (tm *TunnelManager) publishRegistered(reg *TunnelRegistration) {
	if tm.eventBus == nil {
		return
	}
	defer func() { recover() }()
	tm.eventBus.PublishTyped(events.DomainWebhook, events.KindWebhookRegistered, map[string]interface{}{
		"tunnel_uuid": reg.TunnelUUID, "target": string(reg.Target), "target_id": reg.TargetID,
	})
}

func (tm *TunnelManager) GetTunnel(uuid string) (*TunnelRegistration, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	if tm.registrations == nil {
		return nil, fmt.Errorf("tunnel %q not found", uuid)
	}
	reg, ok := tm.registrations[uuid]
	if !ok {
		return nil, fmt.Errorf("tunnel %q not found", uuid)
	}
	return reg, nil
}

func (tm *TunnelManager) UpdateTunnel(uuid string, enabled *bool, description *string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if tm.registrations == nil {
		return fmt.Errorf("tunnel %q not found", uuid)
	}
	reg, ok := tm.registrations[uuid]
	if !ok {
		return fmt.Errorf("tunnel %q not found", uuid)
	}
	if enabled != nil {
		reg.Enabled = *enabled
	}
	if description != nil {
		reg.Description = *description
	}
	reg.UpdatedAt = time.Now().UTC()
	tm.persist()
	return nil
}

func (tm *TunnelManager) ensureInit() {
	if tm.registrations == nil {
		tm.registrations = make(map[string]*TunnelRegistration)
	}
	if tm.bandwidth == nil {
		tm.bandwidth = make(map[string]int64)
	}
}

func (tm *TunnelManager) DeleteTunnel(uuid string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.ensureInit()
	if _, ok := tm.registrations[uuid]; !ok {
		return fmt.Errorf("tunnel %q not found", uuid)
	}
	delete(tm.registrations, uuid)
	delete(tm.bandwidth, uuid)
	tm.persist()
	tm.publishUnregistered(uuid)
	return nil
}

func (tm *TunnelManager) publishUnregistered(uuid string) {
	if tm.eventBus == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			// event bus failure must not take down the RPC call
		}
	}()
	tm.eventBus.PublishTyped(events.DomainWebhook, events.KindWebhookUnregistered, map[string]interface{}{
		"tunnel_uuid": uuid,
	})
}

func (tm *TunnelManager) ListTunnels() []*TunnelRegistration {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	if tm.registrations == nil {
		return nil
	}
	result := make([]*TunnelRegistration, 0, len(tm.registrations))
	for _, r := range tm.registrations {
		result = append(result, r)
	}
	return result
}

func (tm *TunnelManager) RecordActivity(entry WebhookActivityEntry) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.activities = append(tm.activities, entry)
	if len(tm.activities) > tm.maxActivities {
		tm.activities = tm.activities[len(tm.activities)-tm.maxActivities:]
	}
	if tm.bandwidth == nil {
		tm.bandwidth = make(map[string]int64)
	}
	tm.bandwidth[entry.TunnelUUID] += int64(entry.ResponseSize)
}

func (tm *TunnelManager) ListActivities(limit int) []WebhookActivityEntry {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	if limit <= 0 || limit > len(tm.activities) {
		limit = len(tm.activities)
	}
	start := len(tm.activities) - limit
	if start < 0 {
		start = 0
	}
	result := make([]WebhookActivityEntry, limit)
	copy(result, tm.activities[start:])
	return result
}

func (tm *TunnelManager) ClearActivities() {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.activities = tm.activities[:0]
}

func (tm *TunnelManager) GetBandwidth(uuid string) int64 {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	if tm.bandwidth == nil {
		return 0
	}
	return tm.bandwidth[uuid]
}

func (tm *TunnelManager) persist() {
	if tm.persistPath == "" {
		return
	}
	regs := tm.registrations
	if regs == nil {
		regs = make(map[string]*TunnelRegistration)
	}
	data, err := json.Marshal(regs)
	if err != nil {
		slog.Warn("webhook tunnel: failed to marshal registrations", "error", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(tm.persistPath), 0700); err != nil {
		slog.Warn("webhook tunnel: failed to create persist dir", "path", tm.persistPath, "error", err)
		return
	}
	if err := os.WriteFile(tm.persistPath, data, 0600); err != nil {
		slog.Warn("webhook tunnel: failed to write persist file", "path", tm.persistPath, "error", err)
	}
}

func (tm *TunnelManager) load() {
	data, err := os.ReadFile(tm.persistPath)
	if err != nil {
		return
	}
	var regs map[string]*TunnelRegistration
	if err := json.Unmarshal(data, &regs); err != nil {
		return
	}
	if regs != nil {
		tm.registrations = regs
	}
}
