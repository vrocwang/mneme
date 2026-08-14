# Mneme

English | [简体中文](./README.zh-CN.md)

Mneme is an AI agent platform built with [Wails](https://wails.io) (Go backend +
React/TypeScript frontend). It provides a desktop assistant, a CLI, sub-agent
orchestration, a memory/learning pipeline, MCP tool integration, and a layered
security model — with all non-core capabilities delivered as runtime extensions.

## Highlights

- **Multi-channel**: desktop GUI, CLI, plus IRC, Matrix, Mattermost, Lark,
  DingTalk, Email, iMessage, QQ and WhatsApp channels — all as extensions.
- **Sub-agent orchestration**: 12 built-in specialists (general, orchestrator,
  researcher, coder, critic, planner, summarizer, archivist, mcp_setup,
  tool_maker, task_manager, tools_agent) delegating via agent-as-tool.
- **Memory pipeline**: ingestion, multi-strategy retrieval, entity graph,
  profile store, and per-session memory injection into every turn.
- **Learning**: post-turn reflection extracts preferences and experiences that
  shape future responses.
- **Layered security**: prompt-injection detection, autonomy tiers, command
  gating, approval flows, audit logging, sandboxing, credential scrubbing, and
  a narration/failure circuit breaker.
- **Extension system**: 38 runtime extensions (tools, channels, agents,
  integrations) discovered via `manifest.json` and spoken to over JSON-RPC — no
  recompilation needed.
- **MCP**: Model Context Protocol servers managed from the UI, persisted to
  SQLite, and reconnected at boot.

## Build

```bash
# Core build (web + CLI channels, core tools)
go build ./...

# Standalone CLI binary
go build -o mneme-cli ./cmd/mneme-cli/

# Wails desktop GUI
wails build      # production build
wails dev        # live development with hot reload

# Build all extensions
go run cmd/build-extensions/main.go
```

Platform-specific code (PTY, keyring, sandbox, cwd jail) uses OS build tags
internally. Optional features and non-core tools are loaded as runtime
extensions — no feature build tags are required.

## Frontend

```bash
cd frontend
npm install
npm run dev      # Vite dev server
npm run build    # production build (consumed by Wails via embed)
```

## Configuration

Mneme is configured via a TOML file (`config.toml`) in the workspace root. The
workspace holds config, the SQLite database, extensions, logs, and skills.

```toml
workspace = "~/.mneme"

[agent]
default_model = "llama3"
max_output_tokens = 0          # 0 = provider default
temperature = 0
[agent.model_routes]           # task kind -> model override
coding = "deepseek-r1"
reasoning = "deepseek-r1"
summary = "qwen2.5"

[security]
tier = "supervised"            # readonly | supervised | full
workspace_only = false

[[providers]]
name = "ollama"
type = "ollama"                # openai | anthropic | ollama
base_url = "http://localhost:11434"
models = ["llama3", "qwen2.5", "deepseek-r1"]

[[providers]]
name = "openai"
type = "openai"
api_key = "sk-..."
models = ["gpt-4o", "gpt-4o-mini"]

[memory]
max_chunk_size = 0             # 0 = default
retention_days = 0             # 0 = keep forever
max_search_results = 0         # 0 = default

[embedding]
provider = "ollama"            # ollama | openai | "" to disable

[context]
max_tokens = 128000
compaction_threshold = 0.90
keep_recent = 8

[learning_cfg]
enabled = true

[tools]
optional_tools = []            # [] = all, ["none"] = disable all

[[mcp_servers]]
name = "filesystem"
transport = "stdio"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "."]
enabled = true
```

### Autonomy tiers

| Tier | Behavior |
|------|----------|
| `readonly` | Only read-class commands allowed; everything else blocked |
| `supervised` | Reads allowed; writes/execute require approval |
| `full` | Reads and writes allowed; execute requires approval |

## Architecture

```
app.go              - Wails thin proxy (composition root, no logic)
main.go             - Entry point: CLI dispatch + Wails GUI
internal/           - All business logic
  boot/             - Wiring: provider, capability registry, GUI bootstrap
  agent/            - Agent loop, sub-agents, chat service, triage, tasks
  eino/             - Agent creation, runner, tool/provider adapters, middleware
  channels/         - Channel interface + orchestrator (web + CLI core)
  memory/           - Ingestion pipeline, retrieval, embeddings, profile store
  learning/         - Post-turn reflection, preference extraction
  security/         - Injection detection, tier gate, approval, audit, sandbox
  capability/       - Tool + agent registry (Go + MCP + extensions)
  config/           - TOML config parsing + model routing
  context/          - System prompt builder, enrichers, guard, compaction
  inference/        - LLM provider abstraction (streaming + non-streaming)
  prompts/          - Embedded prompt templates (defaults/*.txt)
  mcp/              - MCP server store, registry, tools
  ...               - cron, dag, desktop, monitor, todos, webhooks, workflows
pkg/                - Reusable libraries (events, rpc, tools, embeddings)
cmd/                - Build tools (mneme-cli, build-extensions)
extensions/         - Runtime-loaded extensions (JSON-RPC)
frontend/           - React + TypeScript + Vite + Tailwind
```

### Agent loop

Every turn flows through the eino runner: **security check → agent execution →
event processing → post-turn hooks** (audit, cost, memory extraction, learning
reflection). The same path is used for streaming, checkpointed, and resumed
turns. Each tool execution is wrapped with an approval gate, circuit breaker,
and credential scrubbing — sub-agent delegation is gated the same way.

### Tool composition

The `CapabilityRegistry` is the single source of truth: builtin tools, MCP
servers, and extensions are all registered there. The eino agent set is built in
three phases — create specialists with role-filtered tools, wrap each specialist
as an agent-as-tool, then create the general agent with core + delegation tools.

## Extensions

Extensions are self-contained binaries discovered at runtime. Each extension
directory ships a `manifest.json` declaring its `category`, tools, and agents,
and communicates with the core over stdin/stdout JSON-RPC 2.0.

| Category | Examples |
|----------|----------|
| `channels` | channel-irc, channel-matrix, channel-lark, channel-email, channel-whatsapp-web |
| `integrations` | composio, audio, presentation, browser-cdp, lsp, search-engines |
| `desktop` | desktop-auto |
| `agents` | agent packs (agent.toml + prompt.md) |

Build an extension:

```bash
cd extensions/<name>
go build -ldflags="-s -w" -o <name> .
```

Extensions placed in `extensions/` (or `extensions-dist/`) are discovered
automatically at boot. User-defined agents live in `workspace/agents/*.toml` and
override built-ins with the same ID.

## Channels

Core channels (`web`, `cli`) ship in `internal/channels/`. All other channels
are extensions. Inbound messages are triaged through an eino graph
(rules → cloud LLM → local AI → defer) and dispatched to the chat service.

## Security model

- **Input filtering**: obfuscation-resistant prompt-injection detection
  (leet-speak, homoglyph, zero-width normalization); score ≥ 0.70 blocks.
- **Tool gating**: every tool call passes an approval gate; the shell tool adds
  defense-in-depth tier classification (`CheckGatedCommand`).
- **Credential scrubbing**: tool output is scrubbed of `sk-`, `ghp_`, `xoxb-`,
  `Bearer`, and PEM keys before entering conversation history.
- **Circuit breaker**: trips on repeated tool failures, consecutive failures, or
  narration loops.
- **Audit & sandbox**: security events are audited to SQLite; command execution
  can be sandboxed (Landlock/Seatbelt/AppContainer, cwd jail).

## Memory & learning

The memory pipeline ingests conversations, chunks and embeds them, builds an
entity graph, and exposes multi-strategy retrieval. Before each turn, the memory
middleware injects: recent memory context, user profile facets, tool rules,
skills, workflows, and relevant past experiences. After each turn, background
extraction archives the conversation and the learning engine reflects on it.

## Project layout

```
app.go              - Wails thin proxy (composition root, no logic)
main.go             - Entry point: CLI dispatch + Wails GUI
internal/           - All business logic (60+ packages)
pkg/                - Reusable libraries (events, rpc, tools, embeddings)
cmd/                - Build tools (mneme-cli, build-extensions)
extensions/         - Runtime-loaded extensions (JSON-RPC)
frontend/           - React + TypeScript + Vite + Tailwind
```

See `CLAUDE.md` for the full build guide and architecture notes.
