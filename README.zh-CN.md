# Mneme

[English](./README.md) | 简体中文

Mneme 是一个基于 [Wails](https://wails.io)（Go 后端 + React/TypeScript 前端）构建的 AI
智能体平台。它提供桌面助手、CLI、子智能体编排、记忆/学习流水线、MCP 工具集成，以及分层
安全模型——所有非核心能力均以运行时扩展形式交付。

## 核心特性

- **多通道**：桌面 GUI、CLI，以及 IRC、Matrix、Mattermost、飞书、钉钉、邮件、iMessage、QQ、
  WhatsApp 等通道——全部作为扩展。
- **子智能体编排**：12 个内置专家（general、orchestrator、researcher、coder、critic、
  planner、summarizer、archivist、mcp_setup、tool_maker、task_manager、tools_agent），通过
  “智能体即工具”进行委派。
- **记忆流水线**：摄取、多策略检索、实体图谱、画像存储，以及每轮注入会话记忆。
- **学习**：轮后反思抽取偏好与经验，塑造后续响应。
- **分层安全**：提示注入检测、自治层级、命令门控、审批流、审计日志、沙箱、凭据脱敏，以及
  叙述/失败熔断器。
- **扩展系统**：38 个运行时扩展（工具、通道、智能体、集成）经 `manifest.json` 发现、以
  JSON-RPC 通信——无需重新编译。
- **MCP**：从 UI 管理 Model Context Protocol 服务器，持久化到 SQLite，启动时自动重连。

## 构建

```bash
# 核心构建（web + CLI 通道，核心工具）
go build ./...

# 独立 CLI 二进制
go build -tags "sqlite_fts5" -o mneme-cli ./cmd/mneme-cli/

# Wails 桌面 GUI
wails build      # 生产构建
wails dev        # 实时开发，热重载

# 构建全部扩展
go run cmd/build-extensions/main.go
```

平台相关代码（PTY、密钥环、沙箱、cwd jail）内部使用 OS 构建标签。可选功能与非核心工具
以运行时扩展加载——无需任何特性构建标签。

## 前端

```bash
cd frontend
npm install
npm run dev      # Vite 开发服务器
npm run build    # 生产构建（由 Wails 通过 embed 消费）
```

## 配置

Mneme 通过工作区根目录下的 TOML 文件（`config.toml`）配置。工作区存放配置、SQLite 数据库、
扩展、日志与技能。

```toml
workspace = "~/.mneme"

[agent]
default_model = "llama3"
max_output_tokens = 0          # 0 = 使用 provider 默认
temperature = 0
[agent.model_routes]           # 任务类型 -> 模型覆盖
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
max_chunk_size = 0             # 0 = 默认
retention_days = 0             # 0 = 永久保留
max_search_results = 0         # 0 = 默认

[embedding]
provider = "ollama"            # ollama | openai | "" 禁用

[context]
max_tokens = 128000
compaction_threshold = 0.90
keep_recent = 8

[learning_cfg]
enabled = true

[tools]
optional_tools = []            # [] = 全部，["none"] = 全部禁用

[[mcp_servers]]
name = "filesystem"
transport = "stdio"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "."]
enabled = true
```

### 自治层级

| 层级 | 行为 |
|------|------|
| `readonly` | 仅允许读类命令，其余全部阻断 |
| `supervised` | 读允许；写/执行需审批 |
| `full` | 读写允许；执行需审批 |

## 架构

```
app.go              - Wails 薄代理（组合根，无业务逻辑）
main.go             - 入口：CLI 分发 + Wails GUI
internal/           - 全部业务逻辑
  boot/             - 装配：provider、能力注册表、GUI 引导
  agent/            - 智能体循环、子智能体、聊天服务、分流、任务
  eino/             - 智能体创建、runner、工具/provider 适配、中间件
  channels/         - 通道接口 + orchestrator（web + CLI 核心）
  memory/           - 摄取流水线、检索、嵌入、画像存储
  learning/         - 轮后反思、偏好抽取
  security/         - 注入检测、层级门控、审批、审计、沙箱
  capability/       - 工具 + 智能体注册表（Go + MCP + 扩展）
  config/           - TOML 配置解析 + 模型路由
  context/          - 系统提示构建、富化、守卫、压缩
  inference/        - LLM provider 抽象（流式 + 非流式）
  prompts/          - 内嵌提示模板（defaults/*.txt）
  mcp/              - MCP server store、registry、tools
  ...               - cron、dag、desktop、monitor、todos、webhooks、workflows
pkg/                - 可复用库（events、rpc、tools、embeddings）
cmd/                - 构建工具（mneme-cli、build-extensions）
extensions/         - 运行时加载的扩展（JSON-RPC）
frontend/           - React + TypeScript + Vite + Tailwind
```

### 智能体循环

每一轮都流经 eino runner：**安全检查 -> 智能体执行 -> 事件处理 -> 轮后钩子**
（审计、计费、记忆抽取、学习反思）。流式、检查点、恢复轮次使用同一路径。每次工具执行都
经审批门、熔断器与凭据脱敏包装——子智能体委派同样受门控。

### 工具装配

`CapabilityRegistry` 是唯一事实来源：内置工具、MCP 服务器与扩展均注册于此。eino 智能体集
分三阶段构建——以角色过滤工具创建专家、将每个专家包装为“智能体即工具”、再用核心工具 +
委派工具创建通用智能体。

## 扩展

扩展是运行时发现的自包含二进制。每个扩展目录附带 `manifest.json`，声明其 `category`、工具
与智能体，并通过 stdin/stdout JSON-RPC 2.0 与核心通信。

| 类别 | 示例 |
|------|------|
| `channels` | channel-irc、channel-matrix、channel-lark、channel-email、channel-whatsapp-web |
| `integrations` | composio、audio、presentation、browser-cdp、lsp、search-engines |
| `desktop` | desktop-auto |
| `agents` | agent pack（agent.toml + prompt.md） |

构建扩展：

```bash
cd extensions/<name>
go build -ldflags="-s -w" -o <name> .
```

放置在 `extensions/`（或 `extensions-dist/`）的扩展会在启动时自动发现。用户自定义智能体位于
`workspace/agents/*.toml`，同名时覆盖内置智能体。

## 通道

核心通道（`web`、`cli`）随 `internal/channels/` 发布，其余通道均为扩展。入站消息经 eino 图
（规则 -> 云端 LLM -> 本地 AI -> 延迟）分流，并派发到聊天服务。

## 安全模型

- **输入过滤**：抗混淆的提示注入检测（leet-speak、同形字、零宽字符归一化）；分数 ≥ 0.70 阻断。
- **工具门控**：每次工具调用都过审批门；shell 工具追加纵深防御层级分类（`CheckGatedCommand`）。
- **凭据脱敏**：工具输出在进入会话历史前清除 `sk-`、`ghp_`、`xoxb-`、`Bearer` 与 PEM 密钥。
- **熔断器**：在重复工具失败、连续失败或叙述循环时跳闸。
- **审计与沙箱**：安全事件审计到 SQLite；命令执行可沙箱化（Landlock/Seatbelt/AppContainer、
  cwd jail）。

## 记忆与学习

记忆流水线摄取对话、分块并嵌入、构建实体图谱，并暴露多策略检索。每轮前，记忆中间件注入：
近期记忆上下文、用户画像 facets、工具规则、技能、工作流与相关历史经验。每轮后，后台抽取
归档对话，学习引擎对其进行反思。

## 测试

```bash
go test -tags "sqlite_fts5" ./internal/...
```

核心包与中间件均有测试覆盖：安全门控、熔断器、工具包装、工具过滤、结果转换与失败转移配置。

## 项目结构

```
app.go              - Wails 薄代理（组合根，无业务逻辑）
main.go             - 入口：CLI 分发 + Wails GUI
internal/           - 全部业务逻辑（60+ 包）
pkg/                - 可复用库（events、rpc、tools、embeddings）
cmd/                - 构建工具（mneme-cli、build-extensions）
extensions/         - 运行时加载的扩展（JSON-RPC）
frontend/           - React + TypeScript + Vite + Tailwind
```

完整构建指南与架构说明见 `CLAUDE.md`。
