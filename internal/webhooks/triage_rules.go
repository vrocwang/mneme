package webhooks

import "github.com/simon/mneme/internal/agent"

// DefaultTriageRules returns sensible triage rules for common webhook
// sources. Callers register these into a TriageEvaluator at startup.
// TargetAgent values use bare agent IDs matching CapabilityRegistry entries.
func DefaultTriageRules() []agent.TriageRule {
	return []agent.TriageRule{
		{
			Name:        "github push",
			Sources:     []string{"github"},
			EventKinds:  []string{"push"},
			Keywords:    []string{"refs/heads", "commit"},
			TargetAgent: "coder",
			Priority:    "normal",
			Enabled:     true,
		},
		{
			Name:        "github pull_request",
			Sources:     []string{"github"},
			EventKinds:  []string{"pull_request"},
			TargetAgent: "critic",
			Priority:    "normal",
			Enabled:     true,
		},
		{
			Name:        "github issues",
			Sources:     []string{"github"},
			EventKinds:  []string{"issues"},
			TargetAgent: "planner",
			Priority:    "normal",
			Enabled:     true,
		},
		{
			Name:        "slack message",
			Sources:     []string{"slack"},
			TargetAgent: "general",
			Priority:    "low",
			Enabled:     true,
		},
		{
			Name:        "monitoring alert",
			Sources:     []string{"prometheus", "grafana", "datadog", "pagerduty"},
			Keywords:    []string{"alert", "firing", "critical", "down"},
			TargetAgent: "orchestrator",
			Priority:    "critical",
			Enabled:     true,
		},
		{
			Name:        "cron / automation",
			Sources:     []string{"cron", "automation", "scheduler"},
			TargetAgent: "general",
			Priority:    "low",
			Enabled:     true,
		},
		{
			Name:        "catch-all webhook",
			Sources:     []string{"*"},
			TargetAgent: "general",
			Priority:    "low",
			Enabled:     true,
		},
	}
}
