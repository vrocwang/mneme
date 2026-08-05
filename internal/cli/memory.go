package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
)

func runMemory(args []string) error {
	if len(args) == 0 || (args[0] == "search" && len(args) < 2) {
		fmt.Fprintln(os.Stderr, "Usage: mneme cli memory search <query>")
		return nil
	}

	switch args[0] {
	case "search":
		query := strings.Join(args[1:], " ")
		// Use chat-level bootstrap so the pipeline is available.
		core, err := bootstrapChat()
		if err != nil {
			return fmt.Errorf("bootstrap: %w", err)
		}
		if core.Pipeline == nil {
			return fmt.Errorf("memory pipeline not available; database or provider may be missing")
		}
		result, err := core.Pipeline.Search(context.Background(), query, 20)
		if err != nil {
			return fmt.Errorf("search: %w", err)
		}
		fmt.Println(result.Formatted())
		return nil

	default:
		fmt.Fprintf(os.Stderr, "Unknown: %s\n", args[0])
		fmt.Fprintln(os.Stderr, "Usage: mneme cli memory search <query>")
		return nil
	}
}
