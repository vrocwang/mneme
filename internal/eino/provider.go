// Package eino provides an adapter layer that maps Mneme config to
// cloudwego/eino chat model instances. It handles the three supported
// provider types (openai, anthropic/claude, ollama) and exposes them
// through the standard eino ToolCallingChatModel interface.
package eino

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"

	"github.com/cloudwego/eino-ext/components/model/claude"
	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino-ext/components/model/openai"

	"github.com/simon/mneme/internal/config"
)

// NewChatModel creates an eino ToolCallingChatModel from a ProviderConfig.
//
// It resolves the provider and model name from the config, then delegates
// to the appropriate eino-ext constructor. The returned model implements
// model.ToolCallingChatModel, which supports Generate, Stream, and WithTools.
//
// If the default model is not found in any provider's model list, the
// function falls back to the first configured provider. If no providers
// are configured at all, an error is returned.
func NewChatModel(ctx context.Context, cfg *config.Config) (model.ToolCallingChatModel, error) {
	pc := cfg.FindProviderForModel(cfg.Agent.DefaultModel)
	if pc == nil {
		// Fallback: try to match provider by type name (e.g. model "deepseek-v4-flash" -> provider type "deepseek")
		for i := range cfg.Providers {
			p := &cfg.Providers[i]
			if p.Name != "" && strings.Contains(cfg.Agent.DefaultModel, p.Name) {
				pc = p
				break
			}
		}
	}
	if pc == nil {
		if len(cfg.Providers) > 0 {
			pc = &cfg.Providers[0]
		} else {
			return nil, fmt.Errorf("eino: no provider configured for model %q", cfg.Agent.DefaultModel)
		}
	}

	modelName := cfg.Agent.DefaultModel
	if modelName == "" && len(pc.Models) > 0 {
		modelName = pc.Models[0]
	}

	primary, err := newChatModelFromProvider(ctx, pc, modelName)
	if err != nil {
		return nil, err
	}

	return primary, nil
}

// CollectFailoverModels creates chat models from all configured providers
// except the primary one. These can be used as failover targets in the
// agent's ModelFailoverConfig.
func CollectFailoverModels(ctx context.Context, cfg *config.Config, primary *config.ProviderConfig) ([]model.ToolCallingChatModel, error) {
	var models []model.ToolCallingChatModel
	for i := range cfg.Providers {
		pc := &cfg.Providers[i]
		if pc == primary || pc.Name == primary.Name {
			continue
		}
		if len(pc.Models) == 0 {
			continue
		}
		m, err := newChatModelFromProvider(ctx, pc, pc.Models[0])
		if err != nil {
			continue // skip providers that fail to initialize
		}
		models = append(models, m)
	}
	return models, nil
}

// newChatModelFromProvider creates a single chat model from a provider config.
func newChatModelFromProvider(ctx context.Context, pc *config.ProviderConfig, modelName string) (model.ToolCallingChatModel, error) {
	switch pc.Type {
	case "anthropic", "claude":
		return newClaudeModel(ctx, pc, modelName)
	case "ollama":
		return newOllamaModel(ctx, pc, modelName)
	default: // "openai" or ""
		return newOpenAIModel(ctx, pc, modelName)
	}
}

// newOpenAIModel creates an OpenAI-compatible ChatModel.
func newOpenAIModel(ctx context.Context, pc *config.ProviderConfig, modelName string) (model.ToolCallingChatModel, error) {
	return openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:  pc.APIKey,
		BaseURL: pc.BaseURL,
		Model:   modelName,
	})
}

// newClaudeModel creates an Anthropic Claude ChatModel via the eino-ext
// claude provider. Uses the direct Anthropic API (not Bedrock or Vertex).
func newClaudeModel(ctx context.Context, pc *config.ProviderConfig, modelName string) (model.ToolCallingChatModel, error) {
	// claude.Config.BaseURL is *string; nil means use the default endpoint.
	var baseURL *string
	if pc.BaseURL != "" {
		baseURL = &pc.BaseURL
	}

	return claude.NewChatModel(ctx, &claude.Config{
		APIKey:    pc.APIKey,
		BaseURL:   baseURL,
		Model:     modelName,
		MaxTokens: 4096,
	})
}

// newOllamaModel creates an Ollama ChatModel.
func newOllamaModel(ctx context.Context, pc *config.ProviderConfig, modelName string) (model.ToolCallingChatModel, error) {
	return ollama.NewChatModel(ctx, &ollama.ChatModelConfig{
		BaseURL: pc.BaseURL,
		Model:   modelName,
	})
}

// Ensure the returned models satisfy the eino streaming interface at
// compile time. The concrete types already satisfy model.ToolCallingChatModel,
// which extends model.BaseChatModel with WithTools.
var (
	_ model.BaseChatModel = (*openai.ChatModel)(nil)
	_ model.BaseChatModel = (*claude.ChatModel)(nil)
	_ model.BaseChatModel = (*ollama.ChatModel)(nil)

	// Verify that converting to ToolCallingChatModel is valid.
	_ = func(cm model.ToolCallingChatModel) {} // accept ToolCallingChatModel
)
