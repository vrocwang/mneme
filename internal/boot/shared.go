package boot

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	_ "github.com/simon/mneme/internal/sqlite"

	"github.com/simon/mneme/internal/agent"
	"github.com/simon/mneme/internal/bundle"
	"github.com/simon/mneme/internal/capability"
	"github.com/simon/mneme/internal/channels"
	chancli "github.com/simon/mneme/internal/channels/cli"
	chanweb "github.com/simon/mneme/internal/channels/web"
	"github.com/simon/mneme/internal/config"
	"github.com/simon/mneme/internal/cron"
	einopkg "github.com/simon/mneme/internal/eino"
	"github.com/simon/mneme/internal/inference"
	mcpstore "github.com/simon/mneme/internal/mcp/store"
	"github.com/simon/mneme/internal/memory"
	"github.com/simon/mneme/internal/memory/conversations"
	"github.com/simon/mneme/internal/memory/entities"
	"github.com/simon/mneme/internal/memory/store"
	"github.com/simon/mneme/internal/security"
	ws "github.com/simon/mneme/internal/workspace"
)

// OpenDatabase opens the Mneme SQLite database with WAL mode and busy timeout.
func OpenDatabase(workspace string) (*sql.DB, error) {
	dbPath := filepath.Join(ws.DataDir(workspace), "mneme.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")
	return db, nil
}

// NewProvider creates an inference provider from config.
func NewProvider(cfg *config.Config) inference.Provider {
	p := cfg.FindProviderForModel(cfg.Agent.DefaultModel)
	if p == nil && len(cfg.Providers) > 0 {
		p = &cfg.Providers[0]
	}
	if p == nil {
		return nil
	}

	// Create eino chat model from config and wrap as inference.Provider.
	// This transparently migrates ALL auxiliary LLM consumers (desktop,
	// learning, memory archivist, subconscious, JSON-RPC) to eino.
	ctx := context.Background()
	chatModel, err := einopkg.NewChatModel(ctx, cfg)
	if err != nil || chatModel == nil {
		return nil
	}
	return einopkg.NewEinoProvider(p.Name, chatModel)
}

// NewCapRegistry creates a CapabilityRegistry, runs BootstrapAll, and
// reconnects any persisted MCP servers. Returns the MCP store so callers
// can wire it into RPC handlers.
func NewCapRegistry(cfg *config.Config, db *sql.DB, audit *security.AuditLogger, log *slog.Logger) (*capability.CapabilityRegistry, *mcpstore.Store) {
	reg := capability.NewCapabilityRegistry()
	mcpEntries := make([]capability.ServerEntry, len(cfg.MCPServers))
	for i, s := range cfg.MCPServers {
		mcpEntries[i] = capability.ServerEntry{
			Name: s.Name, Transport: s.Transport, Command: s.Command,
			Args: s.Args, URL: s.URL, Enabled: s.Enabled,
		}
	}
	var mcpStore *mcpstore.Store
	if db != nil {
		var err error
		mcpStore, err = mcpstore.NewStore(db)
		if err != nil {
			log.Warn("mcp store init failed, persistence disabled", "error", err)
			mcpStore = nil
		}
	}
	BootstrapAll(reg, cfg.Workspace, cfg.Security.Tier, mcpEntries, db, cfg, mcpStore, audit, log)

	// Reconnect persisted MCP servers from SQLite on boot.
	ReconnectPersistedServers(reg, mcpStore, log)

	return reg, mcpStore
}

// NewPipeline creates the memory pipeline with embedder, archivist, and entity
// enricher. Late tools that depend on the pipeline (threads, memory, smart_walk)
// are registered by the caller via RegisterLateTools once the registry exists.
func NewPipeline(db *sql.DB, provider inference.Provider, cfg *config.Config, log *slog.Logger) (*memory.Pipeline, *conversations.Store) {
	if db == nil {
		log.Warn("NewPipeline called with nil db, skipping initialization")
		return nil, nil
	}
	convStore, err := conversations.NewStore(db)
	if err != nil {
		log.Warn("conversations store init failed", "error", err)
		return nil, nil
	}
	memStore, err := store.NewStore(db)
	if err != nil {
		log.Warn("memory store init failed", "error", err)
		return nil, convStore
	}

	// Enable memory encryption if a key is available.
	if key, err := security.LoadOrCreateKey(cfg.Workspace); err == nil && len(key) == 32 {
		memStore.EnableEncryption(key)
	}

	pipeline := memory.NewPipelineWithConfig(log, convStore, memStore, db, memory.PipelineConfig{
		WorkerCount:       cfg.Memory.Pipeline.WorkerCount,
		TreeBucketSize:    cfg.Memory.Pipeline.TreeBucketSize,
		ArchiveMsgLimit:   cfg.Memory.Pipeline.ArchiveMsgLimit,
		MaxChunkSize:      cfg.Memory.MaxChunkSize,
		MaxSearchResults:  cfg.Memory.MaxSearchResults,
		FreshnessHalfLife: cfg.Memory.Pipeline.FreshnessHalfLife,
	})
	pipeline.Start()

	// Init entity extraction + knowledge graph for co-occurrence scoring during retrieval.
	if err := pipeline.InitEntities(cfg.Workspace, db); err != nil {
		log.Warn("entity registry init failed, running without entity extraction", "error", err)
	}

	memory.SetupEmbedder(pipeline, cfg.Embedding, log)
	if provider != nil {
		memory.SetupArchivist(pipeline, provider, cfg.ModelForRoute(config.RouteSummary), log)
		pipeline.SetEntityEnricher(entities.NewLLMEnricher(func(ctx context.Context, prompt string) (string, error) {
			tokens, errs := provider.Chat(ctx, inference.ChatRequest{
				Model: cfg.ModelForRoute(config.RouteSummary), MaxTokens: 256, Temperature: 0.1,
				Messages: []inference.Message{{Role: "user", Content: prompt}},
			})
			var resp string
			for tok := range tokens {
				resp += tok.Text
			}
			var chatErr error
			for e := range errs {
				if e != nil {
					chatErr = e
				}
			}
			return resp, chatErr
		}))
	}
	// A named profile selects a preset; an empty profile means the user is
	// supplying explicit numeric weights. Applying both unconditionally made
	// the profile a silent no-op, because the numeric defaults are non-zero.
	if cfg.Memory.RetrievalWeights.Profile != "" {
		pipeline.ApplyRetrievalProfile(cfg.Memory.RetrievalWeights.Profile)
	} else {
		pipeline.ApplyRetrievalWeights(memory.RetrievalWeights{
			FTS5:     cfg.Memory.RetrievalWeights.FTS5,
			Vector:   cfg.Memory.RetrievalWeights.Vector,
			Keyword:  cfg.Memory.RetrievalWeights.Keyword,
			Tree:     cfg.Memory.RetrievalWeights.Tree,
			Graph:    cfg.Memory.RetrievalWeights.Graph,
			Episodic: cfg.Memory.RetrievalWeights.Episodic,
		})
	}

	return pipeline, convStore
}

// NewCronChatSender returns a cron.ChatSender that delegates to
// agent.ChatService. The service is resolved through a getter on every call so
// the sender stays valid whether it is built before startup (Wails Bind list)
// or after (AppCore.Init); a nil getter or nil service surfaces as an error
// instead of a nil sender that silently disables agent-type jobs.
func NewCronChatSender(chatService func() *agent.ChatService) cron.ChatSender {
	return func(ctx context.Context, prompt string) (string, error) {
		if chatService == nil {
			return "", fmt.Errorf("cron: chat service getter not configured")
		}
		cs := chatService()
		if cs == nil {
			return "", fmt.Errorf("cron: chat service not available")
		}
		result, err := cs.SendMessage(agent.WithoutPostHooks(ctx), "cron", prompt)
		if err != nil {
			return "", err
		}
		return result.Response, nil
	}
}

// NewShellRunner returns a cron.ShellRunner that executes commands via the system shell.
func NewShellRunner() cron.ShellRunner {
	return func(ctx context.Context, command string) (string, error) {
		return cron.ExecuteShell(ctx, command)
	}
}

// NewMemoryPrefetcher creates a MemoryPrefetcher backed by the pipeline's tree.
func NewMemoryPrefetcher(pipeline *memory.Pipeline) *agent.MemoryPrefetcher {
	if pipeline == nil {
		return nil
	}
	return agent.NewMemoryPrefetcher(
		agent.NewPipelineTreeAdapter(
			func() []agent.MemoryNodeSummary {
				summaries := pipeline.TreeRootSummaries()
				out := make([]agent.MemoryNodeSummary, len(summaries))
				for i, s := range summaries {
					out[i] = agent.MemoryNodeSummary{ID: s.ID, Content: s.Content, Summary: s.Summary, Count: s.Count}
				}
				return out
			},
			func(query string, limit int) ([]agent.MemoryNodeSummary, error) {
				nodes := pipeline.TreeSearchNodes(query, limit)
				out := make([]agent.MemoryNodeSummary, len(nodes))
				for i, n := range nodes {
					out[i] = agent.MemoryNodeSummary{ID: n.ID, Content: n.Content, Summary: n.Summary, Count: n.Count}
				}
				return out, nil
			},
		),
	)
}

// ProviderDiagnostics returns provider status fields for Health() endpoint.
func ProviderDiagnostics(cfg *config.Config, provider inference.Provider) map[string]interface{} {
	out := map[string]interface{}{}
	if provider != nil {
		out["provider"] = provider.Name()
	} else {
		out["provider"] = "none"
	}
	out["default_model"] = cfg.Agent.DefaultModel
	out["providers_configured"] = len(cfg.Providers)
	return out
}

// VisionModel returns the model for vision/screenshot analysis.
// Uses ModelRoutes["vision"] if configured, otherwise DefaultModel.
func VisionModel(cfg *config.Config) string {
	return cfg.ModelForRoute(config.RouteVision)
}

// SaveUserMessage returns a function that persists a user message via the store getter.
// If the thread doesn't exist yet (e.g. restored from frontend persist without a
// matching DB row), it creates one so the FK constraint on messages is satisfied.
// Persistence failures are logged rather than dropped, so a lost user turn is visible.
func SaveUserMessage(getStore func() *conversations.Store, log *slog.Logger) func(string, string) {
	return func(threadID string, message string) {
		s := getStore()
		if s == nil {
			return
		}
		if err := s.EnsureThread(threadID, message); err != nil && log != nil {
			log.Warn("failed to ensure thread row", "thread", threadID, "error", err)
		}
		if err := s.AddMessage(threadID, "user", message); err != nil && log != nil {
			log.Warn("failed to persist user message", "thread", threadID, "error", err)
		}
	}
}

// RegisterCoreChannels registers always-available channels (web, cli) into
// the CapabilityRegistry so the orchestrator can discover them.
func RegisterCoreChannels(reg *capability.CapabilityRegistry, log *slog.Logger) {
	// Web channel
	reg.RegisterBuiltinChannel(&capability.ChanProviderFunc{
		NameStr: channels.ChannelWeb,
		CreateFn: func(cfg capability.ChannelConfig) (capability.Channel, error) {
			return chanweb.New(log), nil
		},
	})

	// CLI channel
	reg.RegisterBuiltinChannel(&capability.ChanProviderFunc{
		NameStr: channels.ChannelCLI,
		CreateFn: func(cfg capability.ChannelConfig) (capability.Channel, error) {
			return chancli.New(), nil
		},
	})

	log.Info("core channels registered", "channels", reg.ListChannels())
}

// heartbeatInterval returns the configured heartbeat cadence, or the default
// when unset.
func heartbeatInterval(cfg *config.Config) time.Duration {
	if cfg != nil && cfg.Cron.HeartbeatIntervalSecs > 0 {
		return time.Duration(cfg.Cron.HeartbeatIntervalSecs) * time.Second
	}
	return bundle.HeartbeatInterval
}
