package goals

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Store persists goals in a Markdown file (<workspace>/data/MEMORY_GOALS.md).
// The file format is compatible with the Rust openhuman memory_goals module:
//
//	# Goals
//	- [g1] First goal text
//	- [g2] Second goal text
type Store struct {
	path string
	mu   sync.Mutex
}

// NewStore creates a goal store backed by the given file path.
// The file and parent directory are created if they don't exist.
func NewStore(path string) (*Store, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("goals: create data dir: %w", err)
	}
	s := &Store{path: path}
	// Ensure the file exists with a valid header.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte("# Goals\n"), 0644); err != nil {
			return nil, fmt.Errorf("goals: create file: %w", err)
		}
	}
	return s, nil
}

// Path returns the backing file path.
func (s *Store) Path() string { return s.path }

// Load reads and parses the goals file, returning a GoalsDoc.
func (s *Store) Load() (*GoalsDoc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("goals: read file: %w", err)
	}

	var items []GoalItem
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Parse "- [g1] text" format.
		if !strings.HasPrefix(line, "- [") {
			continue
		}
		rest := strings.TrimPrefix(line, "- [")
		idx := strings.Index(rest, "] ")
		if idx < 0 {
			continue
		}
		id := rest[:idx]
		text := strings.TrimSpace(rest[idx+2:])
		if id == "" || text == "" {
			continue
		}
		items = append(items, GoalItem{ID: id, Text: text})
	}

	doc := &GoalsDoc{Items: items, Modified: time.Now()}
	return doc, nil
}

// Save writes the goals to the file.
func (s *Store) Save(doc *GoalsDoc) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var b strings.Builder
	b.WriteString("# Goals\n")
	for _, item := range doc.Items {
		b.WriteString(fmt.Sprintf("- [%s] %s\n", item.ID, item.Text))
	}

	if err := os.WriteFile(s.path, []byte(b.String()), 0644); err != nil {
		return fmt.Errorf("goals: write file: %w", err)
	}
	doc.Modified = time.Now()
	return nil
}

// nextGoalID returns the next available goal ID (g1, g2, ...) based on
// existing items.
func nextGoalID(doc *GoalsDoc) string {
	max := 0
	for _, item := range doc.Items {
		var n int
		if _, err := fmt.Sscanf(item.ID, "g%d", &n); err == nil && n > max {
			max = n
		}
	}
	return fmt.Sprintf("g%d", max+1)
}

// Add appends a new goal with an auto-generated ID and persists it.
func (s *Store) Add(text string) (*GoalItem, error) {
	doc, err := s.Load()
	if err != nil {
		return nil, err
	}
	item := GoalItem{ID: nextGoalID(doc), Text: text}
	doc.Items = append(doc.Items, item)
	if err := s.Save(doc); err != nil {
		return nil, err
	}
	return &item, nil
}

// Edit updates the text of the goal with the given ID and persists it.
func (s *Store) Edit(id, text string) (*GoalItem, error) {
	doc, err := s.Load()
	if err != nil {
		return nil, err
	}
	for i := range doc.Items {
		if doc.Items[i].ID == id {
			doc.Items[i].Text = text
			if err := s.Save(doc); err != nil {
				return nil, err
			}
			return &doc.Items[i], nil
		}
	}
	return nil, fmt.Errorf("goals: goal %q not found", id)
}

// Delete removes the goal with the given ID and persists it.
func (s *Store) Delete(id string) error {
	doc, err := s.Load()
	if err != nil {
		return err
	}
	for i := range doc.Items {
		if doc.Items[i].ID == id {
			doc.Items = append(doc.Items[:i], doc.Items[i+1:]...)
			return s.Save(doc)
		}
	}
	return fmt.Errorf("goals: goal %q not found", id)
}
