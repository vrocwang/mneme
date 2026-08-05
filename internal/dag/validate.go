package dag

import (
	"fmt"
	"strings"
)

// IssueSeverity classifies a validation issue.
type IssueSeverity int

const (
	SeverityError   IssueSeverity = iota // blocks execution
	SeverityWarning                      // surfaced but does not block
)

func (s IssueSeverity) String() string {
	if s == SeverityError {
		return "error"
	}
	return "warning"
}

// ValidationIssue describes a single problem found during graph validation.
type ValidationIssue struct {
	Severity IssueSeverity `json:"severity"`
	NodeID   string        `json:"node_id,omitempty"` // empty for graph-level issues
	Message  string        `json:"message"`
}

func (i ValidationIssue) String() string {
	if i.NodeID != "" {
		return fmt.Sprintf("[%s] node %q: %s", i.Severity, i.NodeID, i.Message)
	}
	return fmt.Sprintf("[%s] %s", i.Severity, i.Message)
}

// ValidateGraph performs deep structural and semantic validation of a DAG
// graph, returning all issues found (not just the first). The basic
// Graph.Validate() is called first; if it passes, additional checks run.
//
// Checks performed:
//   - Basic structural validation (delegates to Graph.Validate)
//   - Entry node existence (at least one node with no incoming edges)
//   - Orphan node detection (non-trigger nodes with no edges at all)
//   - Node config validation (required fields per node kind)
//   - Condition node port routing (true/false edges should exist)
//   - Trigger node placement (must be first / have no incoming edges)
func ValidateGraph(graph *Graph) []ValidationIssue {
	var issues []ValidationIssue

	// Phase 1: basic structural validation.
	if err := graph.Validate(); err != nil {
		if ve, ok := err.(*ValidationError); ok {
			issues = append(issues, ValidationIssue{
				Severity: SeverityError,
				Message:  ve.Message,
			})
		} else {
			issues = append(issues, ValidationIssue{
				Severity: SeverityError,
				Message:  err.Error(),
			})
		}
		return issues // structural errors prevent further checks
	}

	// Phase 2: entry node existence.
	hasEntry := false
	inDegree := make(map[string]int)
	for _, n := range graph.Nodes {
		inDegree[n.ID] = 0
	}
	for _, e := range graph.Edges {
		inDegree[e.ToNode]++
	}
	for _, n := range graph.Nodes {
		if inDegree[n.ID] == 0 {
			hasEntry = true
			break
		}
	}
	if !hasEntry {
		issues = append(issues, ValidationIssue{
			Severity: SeverityError,
			Message:  "graph has no entry node (every node has incoming edges → cycle or missing trigger)",
		})
	}

	// Phase 3: orphan node detection.
	outDegree := make(map[string]int)
	for _, e := range graph.Edges {
		outDegree[e.FromNode]++
	}
	for _, n := range graph.Nodes {
		if isTriggerKind(n.Kind) {
			continue
		}
		if inDegree[n.ID] == 0 && outDegree[n.ID] == 0 && len(graph.Nodes) > 1 {
			issues = append(issues, ValidationIssue{
				Severity: SeverityWarning,
				NodeID:   n.ID,
				Message:  "orphan node (no incoming or outgoing edges)",
			})
		}
	}

	// Phase 4: trigger node placement.
	for _, n := range graph.Nodes {
		if isTriggerKind(n.Kind) && inDegree[n.ID] > 0 {
			issues = append(issues, ValidationIssue{
				Severity: SeverityError,
				NodeID:   n.ID,
				Message:  "trigger node must not have incoming edges",
			})
		}
	}

	// Phase 5: node config validation.
	for _, n := range graph.Nodes {
		nodeIssues := validateNodeConfig(n)
		issues = append(issues, nodeIssues...)
	}

	// Phase 6: condition node port routing.
	for _, n := range graph.Nodes {
		if n.Kind != KindCondition {
			continue
		}
		hasTrue, hasFalse := false, false
		for _, e := range graph.Edges {
			if e.FromNode != n.ID {
				continue
			}
			port := e.FromPort
			if port == "" {
				port = "main"
			}
			if port == "true" {
				hasTrue = true
			}
			if port == "false" {
				hasFalse = true
			}
		}
		if !hasTrue && !hasFalse {
			issues = append(issues, ValidationIssue{
				Severity: SeverityWarning,
				NodeID:   n.ID,
				Message:  "condition node has no true/false port edges (will pass through)",
			})
		} else if !hasTrue {
			issues = append(issues, ValidationIssue{
				Severity: SeverityWarning,
				NodeID:   n.ID,
				Message:  "condition node missing 'true' port edge",
			})
		} else if !hasFalse {
			issues = append(issues, ValidationIssue{
				Severity: SeverityWarning,
				NodeID:   n.ID,
				Message:  "condition node missing 'false' port edge",
			})
		}
	}

	return issues
}

// validateNodeConfig checks required configuration fields for each node kind.
func validateNodeConfig(node Node) []ValidationIssue {
	var issues []ValidationIssue

	switch node.Kind {
	case KindHTTPRequest:
		if _, ok := node.Config["url"]; !ok {
			issues = append(issues, ValidationIssue{
				Severity: SeverityError, NodeID: node.ID,
				Message: "http_request node missing required config: url",
			})
		}
	case KindCondition:
		if _, ok := node.Config["field"]; !ok {
			issues = append(issues, ValidationIssue{
				Severity: SeverityError, NodeID: node.ID,
				Message: "condition node missing required config: field",
			})
		}
		if _, ok := node.Config["op"]; !ok {
			issues = append(issues, ValidationIssue{
				Severity: SeverityError, NodeID: node.ID,
				Message: "condition node missing required config: op",
			})
		}
	case KindCode:
		if source, ok := node.Config["source"]; !ok || source == "" {
			issues = append(issues, ValidationIssue{
				Severity: SeverityError, NodeID: node.ID,
				Message: "code node missing required config: source",
			})
		}
	case KindAgent:
		if prompt, ok := node.Config["prompt"]; !ok || prompt == "" {
			issues = append(issues, ValidationIssue{
				Severity: SeverityError, NodeID: node.ID,
				Message: "agent node missing required config: prompt",
			})
		}
	case KindTransform:
		if _, ok := node.Config["type"]; !ok {
			issues = append(issues, ValidationIssue{
				Severity: SeverityError, NodeID: node.ID,
				Message: "transform node missing required config: type",
			})
		}
	}

	return issues
}

// HasErrors returns true if any issue has SeverityError.
func HasErrors(issues []ValidationIssue) bool {
	for _, i := range issues {
		if i.Severity == SeverityError {
			return true
		}
	}
	return false
}

// FormatIssues returns a human-readable summary of validation issues.
func FormatIssues(issues []ValidationIssue) string {
	if len(issues) == 0 {
		return "no issues found"
	}
	var lines []string
	errors, warnings := 0, 0
	for _, i := range issues {
		lines = append(lines, "  "+i.String())
		if i.Severity == SeverityError {
			errors++
		} else {
			warnings++
		}
	}
	summary := fmt.Sprintf("%d error(s), %d warning(s)", errors, warnings)
	return summary + "\n" + strings.Join(lines, "\n")
}
