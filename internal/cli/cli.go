// Package cli provides the command-line interface for Mneme, matching
// OpenHuman's CLI capabilities: run, call, mcp, agent, memory, tools, health.
package cli

import (
	"fmt"
	"os"
)

const version = "0.2.0"

// Run dispatches CLI subcommands after bootstrapping the minimal subsystem.
func Run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	switch args[0] {
	case "version":
		fmt.Println("Mneme", version)
		return nil

	case "help", "-h", "--help":
		printUsage()
		return nil

	case "run":
		return runServer(args[1:])

	case "call":
		return runCall(args[1:])

	case "mcp":
		return runMCP(args[1:])

	case "agent":
		return runAgent(args[1:])

	case "memory":
		return runMemory(args[1:])

	case "chat":
		return runChat(args[1:])

	case "tools":
		return runTools(args[1:])

	case "subconscious":
		return runSubconscious(args[1:])

	case "health":
		return runHealth()

	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand: %s\n\n", args[0])
		printUsage()
		os.Exit(1)
		return nil
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Mneme — personal AI agent

Usage: mneme <subcommand> [args]

Subcommands:
  chat               Interactive chat with the AI agent (terminal)
  run                Start the JSON-RPC HTTP server + OpenAI-compatible API
  call --tool <name> [--params <json>]
                     Call a registered tool by name
  mcp                Start MCP stdio server (for IDE / MCP client integration)
  agent list         List all registered agents
  agent show <id>    Show agent definition by ID
  memory search <q>  Search the memory pipeline
  tools list         List all registered tools
  subconscious think Run a subconscious evaluation cycle
  subconscious refs  Show recent reflections
  health             Run health checks
  version            Print version
`)
}
