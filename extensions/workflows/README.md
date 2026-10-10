# workflows (Mneme extension)

The core `internal/workflows` package — WORKFLOW.md discovery/authoring plus
workflow run execution — ported out of the core into an extension. It registers
the same tool surface the core registered under `capability.SetWorkflows`, in
the same `category: tools` slot as `doctor`.

## Tools

| tool | permission | effects |
|---|---|---|
| `workflow_list` | read_only | – |
| `workflow_read` | read_only | – |
| `workflow_phase` | read_only | – |
| `workflow_create` | write | yes |
| `workflow_match` | read_only | – |
| `describe_workflow` | read_only | – |
| `run_workflow` | execute | yes |
| `complete_workflow` | execute | – |
| `await_workflow` | none | – |
| `workflow_cancel` | execute | yes |
| `list_workflow_runs` | read_only | – |
| `read_workflow_run_log` | read_only | – |

`workflow_cancel` is the only tool with no core counterpart.

## Paths (identical to the core)

- user workflows: `<workspace>/workflows/<dir>/WORKFLOW.md`
- project workflows: `<workspace>/.workflows/<dir>/WORKFLOW.md`
- run logs: `<workspace>/workflow-logs/<run_id>.log`

`<workspace>` is `extsdk.WorkspaceDir()` (`MNEME_HOME`, exported by the host).

## workflow_cancel

The host cannot cancel an extension call in flight: the protocol is one
request/one response and the host does not forward its context. The core's
`ctx`-based cancellation is therefore unavailable here, so `workflow_cancel`
flips a per-run cancel flag inside this extension. `Runner.Cancel` sets the
flag, cancels the run's own context, and publishes `status=cancelled`; the
completion path refuses to overwrite a cancelled record. `await_workflow` polls
run status every 200ms, so it returns within one tick of the cancel.

Host integration: `internal/tools/extension_proto.go` demultiplexes responses by
JSON-RPC id (one reader goroutine, one pending map), so the host can deliver
`workflow_cancel` while an `await_workflow` call is still outstanding. This
extension enables `srv.SetConcurrentDispatch(true)` so the SDK serves each
`extension.call_tool` in its own goroutine; without that switch the extension
would read one request at a time and could never see the cancel.

## Deliberate deviations from the core

1. `await_workflow` declares `permission: none`. The core tool implemented no
   `PermissionLevel()` method at all (zero value = `PermNone`); omitting the
   field in the extension protocol would be parsed as `execute`, so `none` is
   the faithful mapping.
2. `ConcurrencySafe: true` is declared on the nine tools that had it in core
   (`wait`/read-only tools); `run_workflow`, `workflow_create` and
   `workflow_cancel` are left unsafe. The flag travels over the protocol as
   `concurrency_safe` and reaches the host's tool descriptor.
3. Run IDs are generated locally as UUID-v4 text (crypto/rand) instead of
   `github.com/google/uuid`, so the module needs no extra dependency.
4. The read loop is `srv.Run()` with `SetConcurrentDispatch(true)`; the
   per-request concurrency lives in the SDK rather than in a hand-rolled loop
   here, so every extension that needs mid-call cancellation can opt in.

Runs live in this process's memory (as in the core) and do **not** survive an
extension restart: the log files remain on disk, but old run IDs are no longer
addressable by `list_workflow_runs` / `read_workflow_run_log`.
