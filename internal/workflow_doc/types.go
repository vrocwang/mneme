// Workflow definitions and phase-keyed lifecycle guidance, discovered from
// WORKFLOW.md files.
//
// This package is the CORE's read-only view of the workflow document format: it
// exists so the prompt layer can discover workflows and inject the guidance for
// the current phase. Authoring and execution of workflows live in the
// `workflows` extension, which owns the same on-disk format. The two share no
// code because an extension is a separate Go module and cannot import
// internal/; keep any format change in sync with extensions/workflows.
package workflow_doc

import "time"

// Scope indicates where a workflow was discovered.
type Scope string

const (
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
)

// ToolScope narrows agent tool access for a workflow or phase.
type ToolScope struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

// Phase describes a lifecycle hook within a workflow.
type Phase struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Rules       string    `json:"rules,omitempty"`
	Scripts     []string  `json:"scripts,omitempty"`
	Tools       ToolScope `json:"tools,omitempty"`
	Context     string    `json:"context,omitempty"`
}

// Workflow represents a discovered WORKFLOW.md file.
type Workflow struct {
	Name        string            `json:"name"`
	DirName     string            `json:"dir_name"`
	Description string            `json:"description"`
	Tags        []string          `json:"tags,omitempty"`
	Tools       ToolScope         `json:"tools,omitempty"`
	Phases      map[string]Phase  `json:"phases,omitempty"`
	Scope       Scope             `json:"scope"`
	Frontmatter map[string]string `json:"frontmatter,omitempty"`
	Discovered  time.Time         `json:"discovered_at"`
}

// Summary is a wire-friendly view of a workflow without full phase payloads.
type Summary struct {
	Name        string   `json:"name"`
	DirName     string   `json:"dir_name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	Scope       Scope    `json:"scope"`
	PhaseCount  int      `json:"phase_count"`
}

// Summary returns a lightweight summary of the workflow.
func (w *Workflow) Summary() Summary {
	return Summary{
		Name:        w.Name,
		DirName:     w.DirName,
		Description: w.Description,
		Tags:        w.Tags,
		Scope:       w.Scope,
		PhaseCount:  len(w.Phases),
	}
}

// PhaseNames returns the known phase keys in this workflow.
func (w *Workflow) PhaseNames() []string {
	names := make([]string, 0, len(w.Phases))
	for k := range w.Phases {
		names = append(names, k)
	}
	return names
}
