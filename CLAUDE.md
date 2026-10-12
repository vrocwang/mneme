# Mneme — AI Agent Platform Build Guide

## Build Commands

```bash
# Core build (web + CLI channels, core tools)
go build ./...

# Standalone CLI binary
go build -o mneme-cli ./cmd/mneme-cli/

# Wails desktop GUI
wails build
wails dev    # live development with hot reload

# Extensions (cross-platform)
go run cmd/build-extensions/main.go
```

## Extensions

Optional features (channels, integrations, and non-core tools such as audio,
presentation, LSP, desktop automation, file-state, todos, and workflow
execution) are loaded as runtime extensions via the CapabilityRegistry - no
feature build tags required. The core build includes only web + CLI channels
and the essential tool set. Platform-specific
code (PTY, keyring, sandbox) still uses OS build tags internally.

Composio is loaded as an extension (`extensions/composio/`), not a build tag.

Extensions are loaded at runtime via manifest.json and JSON-RPC: each
extension subdirectory in `extensions/` or `extensions-dist/` is discovered
and registered through the CapabilityRegistry. No recompilation needed.

## Frontend

```bash
cd frontend
# pnpm is pinned in package.json (packageManager); corepack provides it, so no
# global install is needed. wails.json runs the same commands.
corepack pnpm install           # lockfile of record: pnpm-lock.yaml
corepack pnpm run dev           # Vite dev server on :5173
corepack pnpm run build         # TypeScript + Vite production build
# after `corepack enable` you can drop the prefix: pnpm install / pnpm run dev
```

## Architecture

```
app.go              — Wails thin proxy (composition root, no logic)
internal/
  agent/            — Agent loop, sub-agents, chat service, streaming, tools
  boot/             — Wiring: NewProvider, NewCapRegistry, NewPipeline
  eino/             — Agent creation (NewAgentSet), runner, tools adapter, provider adapter
  channels/         — Channel interface + orchestrator + web/cli impls
  config/           — TOML config parsing + model routing
  context/          — System prompt builder, enrichers, guard, compaction
  inference/        — LLM provider abstraction (streaming + non-streaming)
  learning/         — Post-turn reflection, preference extraction
  memory/           — Ingestion pipeline, retrieval, embeddings, profile store
  security/         — Injection detection, tier gate, approval, audit, sandbox
  capability/       — Tool + agent registry (Go + MCP + extensions)
  webhooks/         — Webhook triage + dispatch
  workflow_doc/     — Read-only WORKFLOW.md discovery + phase guidance (prompt)
  prompts/          — Embedded prompt templates (defaults/*.txt)
```

## Sub-agent System

12 built-in agents: general, orchestrator, researcher, coder, critic, planner,
summarizer, archivist, mcp_setup, tool_maker, task_manager, tools_agent.

The built-in roster is `internal/bundle/builtin_agents.toml`, loaded through
the same `agenttoml` path as user agents (`workspace/agents/*.toml`) and agent
packs — all agent sources share one registration path. Each agent's system
prompt lives in `internal/prompts/defaults/agent_<id>.txt` and resolves via
`prompts.NameFromAgentID`. Sub-agents are created by `eino.NewAgentSet` which
builds them in three phases: (1) create the specialist sub-agents with
role-filtered tools and specialized prompts, (2) wrap each as an
`adk.AgentTool` for delegation from the General agent, (3) create the General
agent with all core tools + sub-agent tools.
