package cli

import (
	"encoding/json"
	"fmt"
	"os"
)

func runAgent(args []string) error {
	if len(args) == 0 || args[0] == "list" {
		return listAgentsCLI()
	}
	if args[0] == "show" && len(args) > 1 {
		return showAgentCLI(args[1])
	}
	fmt.Fprintln(os.Stderr, "Usage: mneme agent list")
	fmt.Fprintln(os.Stderr, "       mneme agent show <id>")
	return nil
}

func listAgentsCLI() error {
	core, err := bootstrap()
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	descs := core.CapReg.AllAgents()
	if len(descs) == 0 {
		fmt.Println("No agents registered.")
		return nil
	}
	fmt.Printf("Registered agents (%d):\n\n", len(descs))
	for _, d := range descs {
		hidden := ""
		if d.Hidden {
			hidden = " [hidden]"
		}
		fmt.Printf("  %-20s %-10s %s%s\n", d.ID, d.Tier, d.Name, hidden)
	}
	return nil
}

func showAgentCLI(id string) error {
	core, err := bootstrap()
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	def, ok := core.CapReg.GetAgent(id)
	if !ok {
		return fmt.Errorf("agent %q not found", id)
	}
	b, _ := json.MarshalIndent(map[string]interface{}{
		"id":             def.ID,
		"name":           def.Name,
		"description":    def.Description,
		"tier":           def.Tier,
		"model":          def.Model,
		"temperature":    def.Temperature,
		"max_iterations": def.MaxIterations,
		"tool_allowlist": def.ToolAllowlist,
		"tool_denylist":  def.ToolDenylist,
		"hidden":         def.Hidden,
		"background":     def.Background,
	}, "", "  ")
	fmt.Println(string(b))
	return nil
}
