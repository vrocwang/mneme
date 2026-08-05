package channels

import (
	"log/slog"

	"github.com/simon/mneme/internal/security"
)

// SanitizeInbound runs prompt injection detection on an inbound channel message.
// Returns the (possibly sanitized) content and whether it was blocked.
// Channels should call this before routing a message to the agent.
func SanitizeInbound(content string, log *slog.Logger) (sanitized string, blocked bool) {
	result := security.DetectPromptInjection(content)
	if result.Blocked {
		log.Warn("prompt injection blocked in channel message",
			"severity", result.Severity.String(),
			"flags", result.Flags,
		)
		return "", true
	}
	if result.Sanitized != "" {
		log.Warn("channel message sanitized",
			"severity", result.Severity.String(),
		)
		return result.Sanitized, false
	}
	return content, false
}
