package threads

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// TurnStateStore persists turn state snapshots to disk so interrupted agent
// turns can be recovered (or surfaced as "interrupted" in the UI).
type TurnStateStore struct {
	dir string
	mu  sync.RWMutex
}

// NewTurnStateStore creates a store rooted at the given workspace directory.
func NewTurnStateStore(workspaceDir string) *TurnStateStore {
	return &TurnStateStore{
		dir: filepath.Join(workspaceDir, "memory", "conversations", "turn_states"),
	}
}

func (s *TurnStateStore) ensureDir() error {
	return os.MkdirAll(s.dir, 0755)
}

func (s *TurnStateStore) pathFor(threadID string) string {
	return filepath.Join(s.dir, fmt.Sprintf("%s.json", threadID))
}

// Put writes a turn state snapshot to disk.
func (s *TurnStateStore) Put(state *TurnState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureDir(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal turn state: %w", err)
	}

	tmpPath := s.pathFor(state.ThreadID) + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write turn state: %w", err)
	}
	return os.Rename(tmpPath, s.pathFor(state.ThreadID))
}

// Get reads a turn state snapshot. Returns nil if absent.
func (s *TurnStateStore) Get(threadID string) (*TurnState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.pathFor(threadID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read turn state: %w", err)
	}

	var state TurnState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal turn state: %w", err)
	}
	return &state, nil
}

// Delete removes a turn state snapshot.
func (s *TurnStateStore) Delete(threadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := os.Remove(s.pathFor(threadID))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// List returns all persisted turn state thread IDs.
func (s *TurnStateStore) List() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureDir(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var ids []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		ids = append(ids, e.Name()[:len(e.Name())-5])
	}
	return ids, nil
}

// ClearAll removes all turn state snapshots.
func (s *TurnStateStore) ClearAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			os.Remove(filepath.Join(s.dir, e.Name()))
		}
	}
	return nil
}

// MarkAllInterrupted sets lifecycle to "interrupted" for all stored turn states,
// called at startup to surface surviving snapshots after a crash.
func (s *TurnStateStore) MarkAllInterrupted() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(s.dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var state TurnState
		if err := json.Unmarshal(data, &state); err != nil {
			continue
		}
		if state.Lifecycle == TurnInterrupted {
			continue
		}
		state.Lifecycle = TurnInterrupted
		state.ActiveTool = ""
		newData, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal turn state: %w", err)
		}
		os.WriteFile(path, newData, 0644)
	}
	return nil
}
