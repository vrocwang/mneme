package agent

import "strings"

// ModelResolutionStrategy determines which model a sub-agent should use.
type ModelResolutionStrategy string

const (
	// StrategyInherit uses the parent agent's model.
	StrategyInherit ModelResolutionStrategy = "inherit"
	// StrategyHint picks a model based on the workload type.
	StrategyHint ModelResolutionStrategy = "hint"
	// StrategyExact uses a specific named model.
	StrategyExact ModelResolutionStrategy = "exact"
)

// ModelHint describes the workload to help pick an appropriate model.
type ModelHint string

const (
	HintGeneral   ModelHint = "general"
	HintCoding    ModelHint = "coding"    // prefers models good at code
	HintResearch  ModelHint = "research"  // prefers models good at search/synthesis
	HintSummarize ModelHint = "summarize" // prefers fast, cheap models
	HintCreative  ModelHint = "creative"  // prefers creative writing models
	HintAnalysis  ModelHint = "analysis"  // prefers reasoning models
)

// ModelResolution configures how a sub-agent selects its model.
type ModelResolution struct {
	Strategy ModelResolutionStrategy `json:"strategy"`
	Hint     ModelHint               `json:"hint,omitempty"`  // used with StrategyHint
	Model    string                  `json:"model,omitempty"` // used with StrategyExact
}

// DefaultModelResolution returns the default inherit strategy.
func DefaultModelResolution() *ModelResolution {
	return &ModelResolution{Strategy: StrategyInherit}
}

// ResolveModel determines the actual model name given the resolution config and parent model.
func (r *ModelResolution) ResolveModel(parentModel string, availableModels []string) string {
	if r == nil {
		return parentModel
	}

	switch r.Strategy {
	case StrategyExact:
		if r.Model != "" {
			if containsModel(availableModels, r.Model) {
				return r.Model
			}
		}
		return parentModel

	case StrategyHint:
		return resolveByHint(r.Hint, parentModel, availableModels)

	default: // inherit
		return parentModel
	}
}

// resolveByHint picks a model suited to the workload hint.
// Config routes take priority over hardcoded preferences, matching Rust's
// config.configured_agent_model() precedence.
func resolveByHint(hint ModelHint, fallback string, available []string) string {
	// Check config routes first (highest priority).
	if routeMap != nil {
		if routeKey, ok := hintToRouteKey[hint]; ok {
			if model, ok := routeMap[routeKey]; ok && model != "" {
				return model
			}
		}
	}

	// Model preference order for each hint.
	hintPreferences := map[ModelHint][]string{
		HintCoding:    {"claude-sonnet-4-6", "claude-sonnet-4-5", "gpt-4o", "deepseek-coder"},
		HintResearch:  {"claude-sonnet-4-6", "claude-haiku-4-5", "gpt-4o-mini"},
		HintSummarize: {"claude-haiku-4-5", "gpt-4o-mini", "llama3.2"},
		HintCreative:  {"claude-opus-4-7", "claude-sonnet-4-6", "gpt-4o"},
		HintAnalysis:  {"claude-opus-4-7", "claude-sonnet-4-6", "o1-preview"},
		HintGeneral:   {},
	}

	prefs, ok := hintPreferences[hint]
	if !ok || len(prefs) == 0 {
		return fallback
	}

	for _, pref := range prefs {
		if containsModel(available, pref) {
			return pref
		}
	}
	return fallback
}

// containsModel checks if a model name (case-insensitive) appears in the list.
func containsModel(models []string, target string) bool {
	for _, m := range models {
		if strings.EqualFold(m, target) {
			return true
		}
	}
	return false
}

// ModelCapability tracks what each model is good at for hint resolution.
type ModelCapability struct {
	Name  string
	Hints []ModelHint // what this model is suitable for
}

// routeMap allows config-driven model routing to override hardcoded preferences.
// Set via SetRouteMap before any sub-agent resolution.
var routeMap map[string]string

// SetRouteMap configures model routing from config (e.g. config.ModelRoutes).
// When a route is defined for a hint, it takes priority over hardcoded preferences.
// This matches Rust's config.configured_agent_model() precedence.
func SetRouteMap(routes map[string]string) {
	if routes == nil {
		routeMap = nil
		return
	}
	routeMap = make(map[string]string, len(routes))
	for k, v := range routes {
		routeMap[k] = v
	}
}

// hintToRouteKey maps ModelHint values to config route keys.
var hintToRouteKey = map[ModelHint]string{
	HintCoding:    "coding",
	HintResearch:  "reasoning",
	HintSummarize: "summary",
	HintCreative:  "default",
	HintAnalysis:  "reasoning",
}

// knownModelCapabilities provides hint resolution data for common models.
// Unexported to prevent runtime mutation; use GetKnownModelCapabilities() to read.
var knownModelCapabilities = []ModelCapability{
	{Name: "claude-opus-4-7", Hints: []ModelHint{HintGeneral, HintCreative, HintAnalysis}},
	{Name: "claude-sonnet-4-6", Hints: []ModelHint{HintGeneral, HintCoding, HintResearch, HintCreative, HintAnalysis}},
	{Name: "claude-haiku-4-5", Hints: []ModelHint{HintGeneral, HintSummarize, HintResearch}},
	{Name: "gpt-4o", Hints: []ModelHint{HintGeneral, HintCoding, HintCreative}},
	{Name: "gpt-4o-mini", Hints: []ModelHint{HintGeneral, HintSummarize, HintResearch}},
	{Name: "deepseek-coder", Hints: []ModelHint{HintCoding}},
	{Name: "llama3.2", Hints: []ModelHint{HintGeneral, HintSummarize}},
}

// GetKnownModelCapabilities returns a copy of the known model capabilities list.
func GetKnownModelCapabilities() []ModelCapability {
	result := make([]ModelCapability, len(knownModelCapabilities))
	copy(result, knownModelCapabilities)
	return result
}
