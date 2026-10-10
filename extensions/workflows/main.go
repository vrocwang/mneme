// Workflows extension for Mneme.
//
// This is a faithful port of the core internal/workflows package — WORKFLOW.md
// discovery/authoring plus workflow run execution — out of the core binary. It
// registers the exact same tool names, descriptions, JSON schemas and
// permission/effect flags the core registered under capability.SetWorkflows,
// reads the same directories (<workspace>/workflows and <workspace>/.workflows),
// and writes run logs to the same place (<workspace>/workflow-logs).
//
// One tool is NEW: workflow_cancel. The host→extension protocol carries no
// cancellation signal for an in-flight call, so the core's context-based
// cancellation is unavailable here. workflow_cancel flips a per-run cancel flag
// inside this extension, which releases an in-flight await_workflow promptly.
//
// Protocol plumbing — registration, the manifest, tool defs, the per-call
// timeout, the read loop and response framing — is entirely pkg/extsdk's:
// there is deliberately no dispatcher copy in this file. The extension turns on
// extsdk's concurrent dispatch, because srv.Run()'s default (one request at a
// time) would never read a follow-up workflow_cancel off stdin while a blocking
// await_workflow was being serviced, so the cancel could never release the
// await. With SetConcurrentDispatch(true) the SDK reads continuously and runs
// each extension.call_tool in its own goroutine.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/simon/mneme/pkg/extsdk"
)

// callTimeout matches the core's exposure of await_workflow, which can block
// for a whole run (the runner's default run timeout is 5 minutes) — far longer
// than the SDK's 60s DefaultCallTimeout.
const callTimeout = 5 * time.Minute

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "workflows: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	workspace := extsdk.WorkspaceDir()
	userDir := extsdk.WorkflowsDir()
	logsDir := extsdk.WorkflowLogsDir()

	runner, err := NewRunner(logsDir)
	if err != nil {
		// The core logged and skipped registration here. An extension cannot
		// skip registration, so keep serving with a runner whose log directory
		// could not be created: discovery/authoring tools still work, and
		// run_workflow reports the concrete create-log failure per call.
		fmt.Fprintf(os.Stderr, "workflows: %v\n", err)
		runner = &Runner{runs: make(map[string]*activeRun), logsDir: logsDir}
	}

	srv := extsdk.NewServer(extsdk.Manifest{
		Name:        "workflows",
		Version:     "0.1.0",
		Description: "WORKFLOW.md discovery/authoring and workflow run execution",
	})
	srv.SetCallTimeout(callTimeout)
	// A workflow_cancel must be readable while an await_workflow call is still
	// outstanding; the SDK's sequential default cannot do that. This is the only
	// reason the extension opts in — every call_tool then runs in its own
	// goroutine and responses are still written one line at a time (extsdk).
	srv.SetConcurrentDispatch(true)
	registerTools(srv, runner, userDir, workspace)

	return srv.Run()
}
