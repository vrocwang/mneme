package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/simon/mneme/internal/agent"
	"github.com/simon/mneme/internal/security"
)

func runChat(args []string) error {
	core, err := bootstrapChat()
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	if core.Provider == nil {
		return fmt.Errorf("no inference provider configured; check config.toml [providers]")
	}

	svc := core.ChatService
	model := core.Cfg.Agent.DefaultModel
	ws := core.Cfg.Workspace

	// Build project context header.
	ctxParts := []string{fmt.Sprintf("Mneme · %s · %s / %s", ws, core.Provider.Name(), model)}
	if branch := gitBranch(ws); branch != "" {
		ctxParts = append(ctxParts, fmt.Sprintf("git: %s", branch))
	}
	if core.CapReg != nil {
		ctxParts = append(ctxParts, fmt.Sprintf("%d tools, %d agents",
			len(core.CapReg.ToolNames()), len(core.CapReg.AllAgents())))
	}
	fmt.Println(strings.Join(ctxParts, " · "))
	fmt.Println("Type /help for commands.")

	ctx := context.Background()
	threadID := "cli-chat"
	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		switch input {
		case "/exit", "/quit":
			fmt.Println("bye.")
			return nil
		case "/help":
			fmt.Println("/exit, /quit  — quit")
			fmt.Println("/clear      — new session")
			fmt.Println("/tools      — list available tools")
			fmt.Println("/memory <q> — search memory")
			continue
		case "/clear":
			threadID = "cli-chat-" + fmt.Sprintf("%d", len(threadID))
			fmt.Println("Session cleared.")
			continue
		case "/tools":
			for _, name := range core.CapReg.ToolNames() {
				fmt.Println(" ", name)
			}
			continue
		}

		if strings.HasPrefix(input, "/memory ") {
			query := strings.TrimPrefix(input, "/memory ")
			if core.Pipeline != nil {
				result, err := core.Pipeline.Search(ctx, query, 5)
				if err != nil {
					fmt.Printf("Search error: %v\n", err)
				} else {
					fmt.Println(result.Formatted())
				}
			} else {
				fmt.Println("Memory pipeline not available.")
			}
			continue
		}

		// Prompt injection check.
		if decision := security.EnforcePromptInput(input); decision.Verdict == security.VerdictBlock {
			fmt.Println("[blocked]")
			continue
		}

		// Streaming agent turn.
		done := make(chan struct{})
		svc.StreamMessage(ctx, threadID, input, func(evt agent.StreamEvent) {
			switch evt.Type {
			case "token":
				fmt.Print(evt.Content)
			case "tool_call":
				fmt.Printf("\n  ⚙ %s", evt.Content)
			case "tool_result":
				fmt.Printf(" → %d chars\n", len(evt.Content))
			case "error":
				fmt.Printf("\n  ✗ %s\n", evt.Content)
			case "done":
				fmt.Println()
				close(done)
			}
		})
		<-done
		fmt.Println()
	}
	return scanner.Err()
}

func gitBranch(dir string) string {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
