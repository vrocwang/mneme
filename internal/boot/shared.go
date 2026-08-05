package boot

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"

	"github.com/simon/mneme/internal/agent"
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
	"github.com/simon/mneme/internal/memory/diff"
	"github.com/simon/mneme/internal/memory/entities"
	"github.com/simon/mneme/internal/memory/store"
	"github.com/simon/mneme/internal/security"
)

// OpenDatabase opens the Mneme SQLite database with WAL mode and busy timeout.
func OpenDatabase(workspace string) (*sql.DB, error) {
	dbPath := filepath.Join(workspace, "data", "mneme.db")
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
func NewCapRegistry(cfg *config.Config, db *sql.DB, log *slog.Logger) (*capability.CapabilityRegistry, *mcpstore.Store) {
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
	BootstrapAll(reg, cfg.Workspace, cfg.Security.Tier, mcpEntries, db, cfg, mcpStore, log)

	// Reconnect persisted MCP servers from SQLite on boot.
	ReconnectPersistedServers(reg, mcpStore, log)

	return reg, mcpStore
}

// NewPipeline creates the memory pipeline with embedder, archivist, entity
// enricher, and registers late tools (threads, memory, smart_walk, council).
func NewPipeline(db *sql.DB, provider inference.Provider, cfg *config.Config, capReg *capability.CapabilityRegistry, log *slog.Logger) (*memory.Pipeline, *conversations.Store) {
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

	pipeline := memory.NewPipeline(log, convStore, memStore, db)
	pipeline.Start()

	// Init entity extraction + knowledge graph for co-occurrence scoring during retrieval.
	if err := pipeline.InitEntities(cfg.Workspace, db); err != nil {
		log.Warn("entity registry init failed, running without entity extraction", "error", err)
	}

	memory.SetupEmbedder(pipeline, cfg.Embedding.Provider, cfg.Embedding.BaseURL, cfg.Embedding.APIKey, log)
	if provider != nil {
		memory.SetupArchivist(pipeline, provider, cfg.Agent.DefaultModel, log)
		pipeline.SetEntityEnricher(entities.NewLLMEnricher(func(ctx context.Context, prompt string) (string, error) {
			tokens, errs := provider.Chat(ctx, inference.ChatRequest{
				Model: cfg.Agent.DefaultModel, MaxTokens: 256, Temperature: 0.1,
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
	pipeline.ApplyRetrievalProfile(cfg.Memory.RetrievalWeights.Profile)

	if capReg != nil {
		RegisterLateTools(capReg, convStore, pipeline, provider, cfg.Agent.DefaultModel)
		if diffStore, err := diff.NewStore(db); err == nil {
			diff.RegisterMemoryDiffTools(capReg, diffStore)
		}
	}

	return pipeline, convStore
}

// NewCronChatSender returns a cron.ChatSender that delegates to agent.ChatService.
func NewCronChatSender(chatService *agent.ChatService) cron.ChatSender {
	if chatService == nil {
		return nil
	}
	return func(ctx context.Context, prompt string) (string, error) {
		result, err := chatService.SendMessage(agent.WithoutPostHooks(ctx), "cron", prompt)
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
func ProviderDiagnostics(cfg *config.Config, provider inference.Provider, companionLoop interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	if provider != nil {
		out["provider"] = provider.Name()
	} else {
		out["provider"] = "none"
	}
	out["default_model"] = cfg.Agent.DefaultModel
	out["providers_configured"] = len(cfg.Providers)
	out["companion_available"] = companionLoop != nil
	return out
}

// ResolveCompanionModel returns the model for desktop companion voice/vision.
// Uses the config's ModelRoutes["chat"] if set, otherwise DefaultModel.
func ResolveCompanionModel(cfg *config.Config) string {
	if m, ok := cfg.Agent.ModelRoutes["chat"]; ok && m != "" {
		return m
	}
	return cfg.Agent.DefaultModel
}

// VisionModel returns the model for vision/screenshot analysis.
// Uses ModelRoutes["vision"] if configured, otherwise DefaultModel.
func VisionModel(cfg *config.Config) string {
	if m, ok := cfg.Agent.ModelRoutes["vision"]; ok && m != "" {
		return m
	}
	return cfg.Agent.DefaultModel
}

// LoadHistoryFromGetter returns a LoadHistory function that resolves the store lazily.
func LoadHistoryFromGetter(getStore func() *conversations.Store) func(string, int) []inference.Message {
	return func(threadID string, limit int) []inference.Message {
		s := getStore()
		if s == nil {
			return nil
		}
		msgs, _ := s.GetMessages(threadID, limit)
		out := make([]inference.Message, len(msgs))
		for i, m := range msgs {
			out[i] = inference.Message{Role: m.Role, Content: m.Content}
		}
		return out
	}
}

// SaveUserMessage returns a function that persists a user message via the store getter.
// If the thread doesn't exist yet (e.g. restored from frontend persist without a
// matching DB row), it creates one so the FK constraint on messages is satisfied.
func SaveUserMessage(getStore func() *conversations.Store) func(string, string) {
	return func(threadID string, message string) {
		s := getStore()
		if s != nil {
			s.EnsureThread(threadID, message)
			s.AddMessage(threadID, "user", message)
		}
	}
}

// RegisterCoreChannels registers always-available channels (web, cli) into
// the CapabilityRegistry so the orchestrator can discover them.
func RegisterCoreChannels(reg *capability.CapabilityRegistry, log *slog.Logger) {
	// Web channel
	reg.RegisterBuiltinChannel(&capability.ChanProviderFunc{
		NameStr: "web",
		CreateFn: func(cfg capability.ChannelConfig) (capability.Channel, error) {
			return &channelAdapter{Channel: chanweb.New(log), stop: make(chan struct{})}, nil
		},
	})

	// CLI channel
	reg.RegisterBuiltinChannel(&capability.ChanProviderFunc{
		NameStr: "cli",
		CreateFn: func(cfg capability.ChannelConfig) (capability.Channel, error) {
			return &channelAdapter{Channel: chancli.New(), stop: make(chan struct{})}, nil
		},
	})

	log.Info("core channels registered", "channels", reg.ListChannels())
}

// channelAdapter wraps a channels.Channel as a capability.Channel.
type channelAdapter struct {
	channels.Channel
	stop chan struct{}
}

func (a *channelAdapter) Start(ctx context.Context) error { return a.Channel.Start(ctx) }
func (a *channelAdapter) Stop() error {
	if a.stop != nil {
		select {
		case <-a.stop:
		default:
			close(a.stop)
		}
	}
	return a.Channel.Stop()
}
func (a *channelAdapter) Events() <-chan capability.ChannelMessage {
	raw := a.Channel.Events()
	out := make(chan capability.ChannelMessage, 128)
	go func() {
		defer close(out)
		for {
			select {
			case <-a.stop:
				return
			case m, ok := <-raw:
				if !ok {
					return
				}
				select {
				case out <- capability.ChannelMessage{
					ID: m.ID, Channel: m.Channel, From: m.From,
					Content: m.Content, ReplyTo: m.ReplyTo,
				}:
				case <-a.stop:
					return
				}
			}
		}
	}()
	return out
}
func (a *channelAdapter) Send(ctx context.Context, msg capability.ChannelMessage) error {
	return a.Channel.Send(ctx, channels.Message{
		ID: msg.ID, Channel: msg.Channel, From: msg.From,
		Content: msg.Content, ReplyTo: msg.ReplyTo,
	})
}

// CapToChanAdapter wraps a capability.Channel as a channels.Channel.
func CapToChanAdapter(ch capability.Channel) channels.Channel {
	return &capToChanAdapter{ch: ch, stop: make(chan struct{})}
}

type capToChanAdapter struct {
	ch   capability.Channel
	stop chan struct{}
}

func (a *capToChanAdapter) Name() string                    { return a.ch.Name() }
func (a *capToChanAdapter) Start(ctx context.Context) error { return a.ch.Start(ctx) }
func (a *capToChanAdapter) Stop() error {
	if a.stop != nil {
		select {
		case <-a.stop:
		default:
			close(a.stop)
		}
	}
	return a.ch.Stop()
}
func (a *capToChanAdapter) Events() <-chan channels.Message {
	raw := a.ch.Events()
	out := make(chan channels.Message, 128)
	go func() {
		defer close(out)
		for {
			select {
			case <-a.stop:
				return
			case m, ok := <-raw:
				if !ok {
					return
				}
				select {
				case out <- channels.Message{
					ID: m.ID, Channel: m.Channel, From: m.From,
					Content: m.Content, ReplyTo: m.ReplyTo,
				}:
				case <-a.stop:
					return
				}
			}
		}
	}()
	return out
}
func (a *capToChanAdapter) Send(ctx context.Context, msg channels.Message) error {
	return a.ch.Send(ctx, capability.ChannelMessage{
		ID: msg.ID, Channel: msg.Channel, From: msg.From,
		Content: msg.Content, ReplyTo: msg.ReplyTo,
	})
}
