// Package goals provides persistent long-term goal tracking for the agent.
// Goals are stored as a compact Markdown file (MEMORY_GOALS.md) in the
// workspace data directory, compatible with the Rust openhuman format.
//
// Tools provided: goals_list, goals_add, goals_edit, goals_delete.
package goals

import "time"

// GoalItem is a single long-term goal with a stable short ID.
type GoalItem struct {
	ID   string `json:"id"`   // Stable short id, e.g. "g1"
	Text string `json:"text"` // One-line goal description
}

// GoalsDoc is the deserialised MEMORY_GOALS.md content.
type GoalsDoc struct {
	Items    []GoalItem `json:"items"`
	Modified time.Time  `json:"modified"`
}
