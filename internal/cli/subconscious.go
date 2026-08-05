package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/simon/mneme/internal/subconscious"
)

func runSubconscious(args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: mneme cli subconscious think")
		fmt.Fprintln(os.Stderr, "       mneme cli subconscious reflections [limit]")
		return nil
	}

	core, err := bootstrap()
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}

	switch args[0] {
	case "think":
		return subconsciousThink(core.Cfg.Workspace)
	case "reflections":
		limit := 20
		if len(args) > 1 {
			fmt.Sscanf(args[1], "%d", &limit)
		}
		return subconsciousReflections(core.Cfg.Workspace, limit)
	default:
		fmt.Fprintf(os.Stderr, "Unknown: %s\n", args[0])
		return nil
	}
}

func subconsciousThink(workspace string) error {
	eng := subconscious.NewPersistent(slogDefault(), workspace)
	eng.Register(subconscious.NewMemoryGapEvaluator(slogDefault()))
	eng.Register(subconscious.NewConversationDigestEvaluator(slogDefault()))
	eng.Register(subconscious.NewIdleReminderEvaluator(slogDefault()))

	actions := eng.Think(context.Background())
	if len(actions) == 0 {
		fmt.Println("No actions produced.")
		return nil
	}
	fmt.Printf("Actions (%d):\n\n", len(actions))
	for _, a := range actions {
		b, _ := json.MarshalIndent(a, "", "  ")
		fmt.Println(string(b))
	}
	return nil
}

func subconsciousReflections(workspace string, limit int) error {
	eng := subconscious.NewPersistent(slogDefault(), workspace)
	refs := eng.GetReflections(limit)
	if len(refs) == 0 {
		fmt.Println("No reflections recorded.")
		return nil
	}
	fmt.Printf("Reflections (%d):\n\n", len(refs))
	for _, r := range refs {
		b, _ := json.MarshalIndent(r, "", "  ")
		fmt.Println(string(b))
	}
	return nil
}
