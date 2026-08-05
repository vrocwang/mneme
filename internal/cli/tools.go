package cli

import "fmt"

func runTools(args []string) error {
	core, err := bootstrap()
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	descs := core.CapReg.AllTools()
	if len(descs) == 0 {
		fmt.Println("No tools registered.")
		return nil
	}
	fmt.Printf("Registered tools (%d):\n\n", len(descs))
	for _, d := range descs {
		perm := d.Permission
		if perm == "" {
			perm = "none"
		}
		effects := ""
		if d.HasSideEffects {
			effects = " [external]"
		}
		fmt.Printf("  %-30s %-10s %s%s\n", d.Name, perm, truncate(d.Description, 60), effects)
	}
	return nil
}
