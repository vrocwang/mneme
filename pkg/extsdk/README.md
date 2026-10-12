# extsdk — extension author contract

`pkg/extsdk` is the SDK every Mneme runtime extension is built on. An extension
is a **separate Go module** (see `extensions/doctor` for the smallest example)
that the host launches as a subprocess and speaks to over **line-delimited
JSON-RPC 2.0 on stdin/stdout**.

Read this before writing an extension: the constraints below are enforced by the
host and are easy to violate accidentally.

## What an extension can and cannot do

An extension is a **leaf request/response server**:

- it answers host-initiated `extension.*` calls;
- it runs with the host's environment, so it shares the workspace
  (`MNEME_HOME`) and may open `<workspace>/data/mneme.db` or read
  `<workspace>/config/config.toml` itself;
- it **cannot call back into the host** and the host **never pushes** to it.
  There is no host RPC, no event delivery and no cancellation signal for a call
  in flight. Anything that needs the live capability registry, the agent loop,
  the event bus or host LLM inference must stay in the core.

Consequences worth knowing before you design a tool:

- stdout is the protocol. Write logs to **stderr** (`slog` defaults to stderr;
  `srv.SetLogger` replaces it).
- The host bounds each call: tools with no declared budget use the host's global
  tool timeout (120s by default). Declare `ToolDef.TimeoutSecs` when a tool
  legitimately runs longer (see below).
- A long-blocking tool can only be interrupted by another call arriving
  concurrently — enable concurrent dispatch and expose a separate cancel tool.

## Minimal extension

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/simon/mneme/pkg/extsdk"
)

func main() {
    srv := extsdk.NewServer(extsdk.Manifest{
        Name: "hello", Version: "0.1.0", Description: "greets",
    })
    srv.RegisterTool(extsdk.ToolDef{
        Name:        "hello_greet",
        Description: "Greet someone.",
        Parameters: map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "name": map[string]interface{}{"type": "string"},
            },
            "required": []string{"name"},
        },
        Permission: "read_only",
    }, func(ctx context.Context, args map[string]interface{}) extsdk.Result {
        return extsdk.Result{Success: true, Output: "hello " + extsdk.GetStr(args, "name", "world")}
    })
    if err := srv.Run(); err != nil {
        fmt.Fprintf(os.Stderr, "hello: %v\n", err)
        os.Exit(1)
    }
}
```

`go.mod` mirrors the other extensions:

```
module github.com/simon/mneme/extensions/hello

go 1.25.0

require github.com/simon/mneme v0.0.0

replace github.com/simon/mneme => ../../
```

`manifest.json` is what the host discovers (see `extensions/MANIFEST.md` for the
field reference). Build every extension with
`go run cmd/build-extensions/main.go`; `wails dev` also auto-builds a missing
binary when the extension directory has a `go.mod`.

## Protocol methods

| Method | Request | Response |
|---|---|---|
| `extension.describe` | — | `Manifest` (name, version, description, `tools`, `agent_defs`, `config_schema`, `protocol_min`) |
| `extension.list_tools` | — | `{"tools": [ToolDef…]}` |
| `extension.list_agents` | — | `{"agents": [AgentDef…]}` |
| `extension.configure` | `{"config": {…}}` | `{"accepted": bool, "error": "…"}` |
| `extension.call_tool` | `{"name": "…", "args": {…}}` | `Result` |

A malformed line is dropped; an unknown method returns JSON-RPC error `-32601`;
an unknown tool returns a `Result` with `error` set. The host sends
`extension.configure` once at launch with the extension's **saved config** (from
the UI/`ConfigRPC`); install it with `SetConfigHandler` and return an error to
reject a config. `Manifest.ProtocolMin` is checked against the host's protocol
version, so an extension requiring a newer protocol is refused rather than
silently misbehaving.

## ToolDef fields

| Field | Meaning |
|---|---|
| `Name`, `Description`, `Parameters` | Exactly what the model sees. `Parameters` is a JSON Schema object. |
| `Permission` | `none` / `read_only` / `write` / `execute` / `dangerous`. Drives the approval gate's risk level. **Declare it**: leaving it empty makes the host parse `""` as `execute` (fail-closed but coarse). |
| `HasEffects` | True when the tool changes state outside the agent. Drives the write-surface/diagnostics view and the side-effect gate. |
| `ConcurrencySafe` | True only when overlapping calls are safe. The host maps it onto `tools.ConcurrencySafeTool`. |
| `TimeoutSecs` | How long the tool may legitimately run (e.g. `300` for a workflow await). Clamped by the host to 1s..1h; an explicit per-call override still wins. Zero means "use the host's global timeout". |

## AgentDef fields

`RegisterAgent(AgentDef{…})` contributes an agent to the host's roster; the model
can delegate to it. Honoured: `ID`, `Name`, `Description`, `SystemPrompt`,
`ToolAllowlist`, `ToolDenylist`, `MaxIterations`, `Model`, `Hidden`,
`ToolPolicy.DenyTools`, `ToolPolicy.MaxToolRounds`. **Not enforced** (they are
accepted for the wire format and warned about at boot): `ForkMode`,
`ExtraTools`, `SkillFilter`, `SubagentRefs`, `Temperature`, `TimeoutSecs`,
`SandboxMode`, `Background`, `ToolPolicy.RequireApprovalFor` — use a tool's
`Permission` instead of a per-agent approval list.

## Workspace paths

Never hand-roll workspace paths. The helpers follow `MNEME_HOME` (exported by the
host) and create what they return, so an extension writes to the same place the
core does:

`WorkspaceDir()`, `DataDir()`, `DataFile(name)`, `SkillsDir()`,
`ScreenshotsDir()`, `TaskBoardsDir()`, `WorkflowsDir()`, `WorkflowLogsDir()`.

A project may additionally keep workflows in `<workspace>/.workflows`; the
workflow discovery code (core and `extensions/workflows`) resolves that path
itself from the workspace root.

## Concurrency, shutdown and timeouts

- Default dispatch is **sequential**: one request at a time. That is fine for
  short tools.
- `SetConcurrentDispatch(true)` serves each `extension.call_tool` in its own
  goroutine (responses are still framed one line at a time). Enable it only when
  handlers are safe to overlap; it is what makes a mid-call cancel tool possible.
- `SetCallTimeout(d)` bounds each handler (`DefaultCallTimeout` is 60s). A tool's
  `TimeoutSecs` tells the *host* how long to wait; `SetCallTimeout` bounds the
  handler inside the extension. Set both when a tool is long-running.
- On stdin EOF `Run` returns after at most 10s of draining in-flight calls; the
  host closes stdin on shutdown.

## Testing an extension locally

Drive the binary directly with the same framing the host uses — no host build
needed:

```bash
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"extension.describe"}' \
  '{"jsonrpc":"2.0","id":2,"method":"extension.list_tools"}' \
  '{"jsonrpc":"2.0","id":3,"method":"extension.call_tool","params":{"name":"hello_greet","args":{"name":"Ada"}}}' \
  | MNEME_HOME=$(mktemp -d) ./hello
```

Check both the success and the failure path of every tool: a tool that reports a
problem must return `Result{Error: …}` (the host feeds that back to the model so
it can correct course) rather than crashing the process. An informational answer
("no matches") is still a success — set `Success: true`, because a zero-valued
`Result` looks like a failure to the host.
