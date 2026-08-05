package agent

import "fmt"

// AgentError is a typed error for agent execution failures.
type AgentError struct {
	Kind    ErrorKind
	Message string
	Cause   error
}

// ErrorKind classifies agent errors for caller handling.
type ErrorKind string

const (
	ErrProvider       ErrorKind = "provider"
	ErrMaxIterations  ErrorKind = "max_iterations"
	ErrCircuitBreaker ErrorKind = "circuit_breaker"
)

func (e *AgentError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Kind, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Kind, e.Message)
}

func (e *AgentError) Unwrap() error { return e.Cause }

// Transient returns true if this error is likely to resolve on retry.
func (e *AgentError) Transient() bool {
	if e.Cause != nil {
		return IsTransientError(e.Cause)
	}
	return e.Kind == ErrProvider
}

// IsTransientError classifies provider-level error strings as transient or permanent.
func IsTransientError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	permanentMarkers := []string{
		"401", "403", "invalid api key", "incorrect api key",
		"model not found", "does not exist", "content policy",
		"safety", "blocked", "moderation", "invalid_request_error",
		"insufficient_quota", "billing", "payment",
	}
	for _, m := range permanentMarkers {
		if containsFold(msg, m) {
			return false
		}
	}
	transientMarkers := []string{
		"429", "rate limit", "too many requests",
		"503", "502", "500", "server error", "overloaded",
		"timeout", "deadline exceeded", "connection refused",
		"temporarily unavailable", "try again",
	}
	for _, m := range transientMarkers {
		if containsFold(msg, m) {
			return true
		}
	}
	return true // fail-open for retry; circuit breaker catches persistent failures
}

func containsFold(s, substr string) bool {
	return len(s) >= len(substr) && indexFold(s, substr) >= 0
}

func indexFold(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if equalFold(s[i:i+len(substr)], substr) {
			return i
		}
	}
	return -1
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 32
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}

func NewProviderError(msg string, cause error) *AgentError {
	return &AgentError{Kind: ErrProvider, Message: msg, Cause: cause}
}

func NewMaxIterationsError(max int) *AgentError {
	return &AgentError{Kind: ErrMaxIterations, Message: fmt.Sprintf("exceeded max iterations (%d)", max)}
}

func NewCircuitBreakerError(msg string) *AgentError {
	return &AgentError{Kind: ErrCircuitBreaker, Message: msg}
}
