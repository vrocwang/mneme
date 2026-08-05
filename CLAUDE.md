# Mneme — AI Agent Platform Build Guide

## Build Commands

```bash
# Core build (web + CLI channels, core tools)
go build ./...

# Standalone CLI binary
go build -tags "sqlite_fts5" -o mneme-cli ./cmd/mneme-cli/

# Wails desktop GUI
wails build
wails dev    # live development with hot reload

# Extensions (cross-platform)
go run cmd/build-extensions/main.go
```

## Extensions

Optional features (channels, integrations, and non-core tools such as audio,
presentation, LSP, and desktop automation) are loaded as runtime extensions
via the CapabilityRegistry - no feature build tags required. The core build
includes only web + CLI channels and the essential tool set. Platform-specific
code (PTY, keyring, sandbox, cwd jail) still uses OS build tags internally.

Composio is loaded as an extension (`extensions/composio/`), not a build tag.
Its in-process memory-sync connector in `internal/memory/sync/composio/` was
dead code (never wired) and has been removed.

Extensions are loaded at runtime via manifest.json and JSON-RPC: each
extension subdirectory in `extensions/` or `extensions-dist/` is discovered
and registered through the CapabilityRegistry. No recompilation needed.

## Frontend

```bash
cd frontend
npm install
npm run dev      # Vite dev server on :5173
npm run build    # TypeScript + Vite production build
```

## Architecture

```
app.go              — Wails thin proxy (composition root, no logic)
internal/
  agent/            — Agent loop, sub-agents, chat service, streaming, tools
  boot/             — Wiring: NewProvider, NewCapRegistry, NewPipeline
  eino/             — Agent creation (NewAgentSet), runner, tools adapter, provider adapter
  channels/         — Channel interface + orchestrator + per-platform impls
  config/           — TOML config parsing + model routing
  context/          — System prompt builder, enrichers, guard, compaction
  inference/        — LLM provider abstraction (streaming + non-streaming)
  learning/         — Post-turn reflection, preference extraction
  memory/           — Ingestion pipeline, retrieval, embeddings, profile store
  security/         — Injection detection, tier gate, approval, audit, sandbox
  capability/       — Tool + agent registry (Go + MCP + extensions)
  model_council/    — Multi-model deliberation
  webhooks/         — Webhook triage + dispatch
  workflows/        — Workflow runner + tools
  prompts/          — Embedded prompt templates (defaults/*.txt)
```

## Sub-agent System

12 built-in agents: general, orchestrator, researcher, coder, critic, planner,
summarizer, archivist, mcp_setup, tool_maker, task_manager, tools_agent.

Each agent's system prompt lives in `internal/prompts/defaults/agent_<id>.txt`.
Sub-agents are created by `eino.NewAgentSet` which builds all 12 agents in three
phases: (1) create 11 sub-agents with role-filtered tools and specialized prompts,
(2) wrap each as an `adk.AgentTool` for delegation from the General agent,
(3) create the General agent with all core tools + sub-agent tools.
