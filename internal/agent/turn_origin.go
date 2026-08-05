package agent

import "context"

// TurnOriginKind classifies where an agent turn was triggered from.
// This drives origin-aware security decisions in the approval gate.
type TurnOriginKind string

const (
	// OriginWebChat — user-initiated turn from the web UI. User is present.
	OriginWebChat TurnOriginKind = "web_chat"

	// OriginExternalChannel — message from an external messaging channel (Slack, Telegram, etc.).
	// User may or may not be present; trust level varies by channel.
	OriginExternalChannel TurnOriginKind = "external_channel"

	// OriginTrustedAutomation — turn triggered by an internal scheduler (cron, subconscious, heartbeat).
	// No user present; highest trust when source is known.
	OriginTrustedAutomation TurnOriginKind = "trusted_automation"

	// OriginCLI — turn from the command-line interface.
	OriginCLI TurnOriginKind = "cli"

	// OriginUnknown — turn origin could not be determined. Treated with maximum caution.
	OriginUnknown TurnOriginKind = "unknown"
)

// Automation source well-known values for TurnOrigin.AutomationSource.
const (
	AutoSourceCron                = "cron"
	AutoSourceSubconscious        = "subconscious"
	AutoSourceSubconsciousTainted = "subconscious_tainted" // external-sync data mixed in
)

// TurnOrigin carries metadata about where an agent turn came from.
type TurnOrigin struct {
	Kind     TurnOriginKind `json:"kind"`
	ThreadID string         `json:"thread_id,omitempty"`
	ClientID string         `json:"client_id,omitempty"`

	// External channel details (set when Kind == OriginExternalChannel).
	Channel string `json:"channel,omitempty"` // "slack", "discord", "telegram", etc.
	Sender  string `json:"sender,omitempty"`  // user ID on the external platform

	// Automation details (set when Kind == OriginTrustedAutomation).
	AutomationSource string `json:"automation_source,omitempty"` // well-known constants above
	JobID            string `json:"job_id,omitempty"`            // cron job ID or equivalent

	// Arbitrary metadata for future extension.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// IsSubconsciousTainted returns true when the turn origin is a subconscious
// tick whose context contains externally-synced memory data. These turns must
// not be allowed to execute external-effect tools without user approval.
func (o TurnOrigin) IsSubconsciousTainted() bool {
	return o.Kind == OriginTrustedAutomation && o.AutomationSource == AutoSourceSubconsciousTainted
}

// IsUserPresent returns true if a human user is expected to be available to
// respond to prompts (web chat or CLI).
func (o TurnOrigin) IsUserPresent() bool {
	return o.Kind == OriginWebChat || o.Kind == OriginCLI
}

// IsAutomation returns true if the turn is from a trusted internal system.
func (o TurnOrigin) IsAutomation() bool {
	return o.Kind == OriginTrustedAutomation
}

// IsExternal returns true if the turn came from a third-party messaging platform.
func (o TurnOrigin) IsExternal() bool {
	return o.Kind == OriginExternalChannel
}

type contextKey struct{}

var turnOriginKey contextKey

// WithTurnOrigin attaches a TurnOrigin to a context.
func WithTurnOrigin(ctx context.Context, origin TurnOrigin) context.Context {
	return context.WithValue(ctx, turnOriginKey, origin)
}

// TurnOriginFromCtx extracts the TurnOrigin from a context.
// Returns OriginUnknown if none was set.
func TurnOriginFromCtx(ctx context.Context) TurnOrigin {
	if v := ctx.Value(turnOriginKey); v != nil {
		return v.(TurnOrigin)
	}
	return TurnOrigin{Kind: OriginUnknown}
}

// ── Origin-aware approval defaults ──────────────────────────────────────────

// OriginPolicy maps turn origins to approval behaviour.
type OriginPolicy struct {
	// AutoApproveTools lists tool names that skip approval for this origin.
	AutoApproveTools []string `json:"auto_approve_tools,omitempty"`
	// RequireApprovalForAll forces every tool call through the gate.
	RequireApprovalForAll bool `json:"require_approval_for_all,omitempty"`
	// MaxRiskLevel is the highest PermissionLevel auto-approved without prompt.
	MaxRiskLevel string `json:"max_risk_level,omitempty"` // "none", "read_only", "write"
}

// DefaultOriginPolicies returns sensible per-origin defaults.
func DefaultOriginPolicies() map[TurnOriginKind]OriginPolicy {
	return map[TurnOriginKind]OriginPolicy{
		OriginWebChat: {
			MaxRiskLevel: "read_only",
		},
		OriginExternalChannel: {
			RequireApprovalForAll: true,
			MaxRiskLevel:          "none",
		},
		OriginTrustedAutomation: {
			MaxRiskLevel: "write",
		},
		OriginCLI: {
			MaxRiskLevel: "read_only",
		},
		OriginUnknown: {
			RequireApprovalForAll: true,
		},
	}
}
