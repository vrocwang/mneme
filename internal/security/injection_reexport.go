package security

import "github.com/simon/mneme/internal/prompt_injection"

// Re-export prompt injection types and functions for backward compatibility.
// The implementation lives in internal/prompt_injection/detect.go.
// New code should import prompt_injection directly.

type InjectionSeverity = prompt_injection.InjectionSeverity
type InjectionResult = prompt_injection.InjectionResult
type PromptEnforcementVerdict = prompt_injection.PromptEnforcementVerdict
type PromptEnforcementAction = prompt_injection.PromptEnforcementAction
type InjectionReason = prompt_injection.InjectionReason
type PromptEnforcementDecision = prompt_injection.PromptEnforcementDecision

const (
	InjectionNone   = prompt_injection.InjectionNone
	InjectionLow    = prompt_injection.InjectionLow
	InjectionMedium = prompt_injection.InjectionMedium
	InjectionHigh   = prompt_injection.InjectionHigh

	VerdictAllow  = prompt_injection.VerdictAllow
	VerdictBlock  = prompt_injection.VerdictBlock
	VerdictReview = prompt_injection.VerdictReview

	ActionAllow         = prompt_injection.ActionAllow
	ActionBlocked       = prompt_injection.ActionBlocked
	ActionReviewBlocked = prompt_injection.ActionReviewBlocked
)

func DetectPromptInjection(input string) InjectionResult {
	return prompt_injection.DetectPromptInjection(input)
}

func QuickInjectionCheck(input string) bool {
	return prompt_injection.QuickInjectionCheck(input)
}

func EnforcePromptInput(input string) PromptEnforcementDecision {
	return prompt_injection.EnforcePromptInput(input)
}

func ScanToolDefinition(name, description string) (float64, []string) {
	return prompt_injection.ScanToolDefinition(name, description)
}
