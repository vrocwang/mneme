package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/simon/mneme/internal/agent"
	"github.com/simon/mneme/internal/security"
)

// turnInterrupts tracks whether a streaming turn is currently in flight so a
// Ctrl-C cancels that turn instead of killing the process. The first SIGINT
// cancels the running turn; a second one exits, matching the "press again to
// force quit" convention. Outside a turn a signal exits immediately, which is
// what the default disposition would do.
type turnInterrupts struct {
	mu      sync.Mutex
	active  bool
	cancel  func()
	presses int
}

// begin marks a turn active and installs its cancel function. It resets the
// press counter so each turn gets its own first-press/second-press semantics.
func (t *turnInterrupts) begin(cancel func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active = true
	t.cancel = cancel
	t.presses = 0
}

// end clears the active turn.
func (t *turnInterrupts) end() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active = false
	t.cancel = nil
}

// signal handles one interrupt. It returns true when the process should exit.
func (t *turnInterrupts) signal() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active {
		return true
	}
	t.presses++
	if t.presses > 1 {
		return true
	}
	fmt.Fprintln(os.Stderr, "\n  cancelling…")
	if t.cancel != nil {
		t.cancel()
	}
	return false
}

func runChat(args []string) error {
	core, err := bootstrapChat()
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	if core.Provider == nil {
		return fmt.Errorf("no inference provider configured; check config.toml [providers]")
	}
	if core.Runner == nil {
		return fmt.Errorf("agent runner unavailable (eino integration failed); check the startup log for provider errors")
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

	// Ctrl-C during a streaming turn must cancel that turn, not kill the
	// process: the chat service's interrupt channel is the supported path and
	// is what the GUI's CancelMessage uses. A second Ctrl-C still exits.
	sigCh := make(chan os.Signal, 4)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	interrupts := &turnInterrupts{}
	stopSignals := make(chan struct{})
	defer close(stopSignals)
	go func() {
		for {
			select {
			case <-stopSignals:
				return
			case sig := <-sigCh:
				if interrupts.signal() {
					fmt.Fprintf(os.Stderr, "\nreceived %s, exiting.\n", sig)
					os.Exit(130)
				}
			}
		}
	}()

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

		// Streaming agent turn. Every terminal event (done or error) carries
		// Done=true; the turn must end on either, otherwise a model/network
		// error would leave the loop blocked on <-done forever. A sync.Once
		// keeps a duplicate terminal event from panicking on a double close.
		done := make(chan struct{})
		var closeOnce sync.Once
		turnCtx, turnCancel := context.WithCancel(ctx)
		// Register the interrupt first: StreamMessage publishes the turn's
		// interrupt channel synchronously before it returns, so a Ctrl-C that
		// lands between the two calls is not lost (the context cancel covers it).
		interrupts.begin(func() {
			svc.Cancel(threadID)
			turnCancel()
		})
		svc.StreamMessage(turnCtx, threadID, input, func(evt agent.StreamEvent) {
			switch evt.Type {
			case agent.StreamToken:
				fmt.Print(evt.Content)
			case agent.StreamToolCall:
				fmt.Printf("\n  ⚙ %s", evt.Content)
			case agent.StreamToolResult:
				// A tool-level failure is fed back to the model as tool output so
				// it can correct course; show it as a failure, not a result.
				if evt.Error != "" {
					fmt.Printf(" ✗ %s\n", evt.Error)
				} else {
					fmt.Printf(" → %d chars\n", len(evt.Content))
				}
			case agent.StreamError:
				fmt.Printf("\n  ✗ %s\n", evt.Content)
			case agent.StreamDone:
				fmt.Println()
			}
			if evt.Done {
				closeOnce.Do(func() { close(done) })
			}
		})
		<-done
		interrupts.end()
		turnCancel()
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
