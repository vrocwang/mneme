package desktop

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/simon/mneme/internal/inference"
)

// VisionEngine provides LLM-powered screen understanding independent of
// the companion loop. It analyzes screenshots and accessibility trees to
// produce structured descriptions of what's on screen.
type VisionEngine struct {
	log      *slog.Logger
	provider inference.Provider
	model    string
}

// VisionConfig holds dependencies for the vision engine.
type VisionConfig struct {
	Provider inference.Provider
	Model    string
	Logger   *slog.Logger
}

// NewVisionEngine creates a screen vision engine.
func NewVisionEngine(cfg VisionConfig) *VisionEngine {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Model == "" {
		cfg.Model = "default"
	}
	return &VisionEngine{
		log:      cfg.Logger.With("component", "vision-engine"),
		provider: cfg.Provider,
		model:    cfg.Model,
	}
}

// ScreenDescription is the structured output of screen analysis.
type ScreenDescription struct {
	ActiveApp    string      `json:"active_app"`
	WindowTitle  string      `json:"window_title"`
	Elements     []UIElement `json:"elements"`
	Summary      string      `json:"summary"`
	IsActionable bool        `json:"is_actionable"`
	Error        string      `json:"error,omitempty"`
}

// UIElement describes a single interactive element on screen.
type UIElement struct {
	Role     string `json:"role"`     // button, text field, link, etc.
	Label    string `json:"label"`    // visible text on the element
	Position string `json:"position"` // e.g. "top-left", "center"
	Action   string `json:"action"`   // what clicking/interacting would do
}

// AnalyzeScreen takes a screenshot path and optionally accessibility context
// and returns a structured description of the screen content.
func (ve *VisionEngine) AnalyzeScreen(ctx context.Context, screenshotPath string, axContext string) (*ScreenDescription, error) {
	if ve.provider == nil {
		return ve.fallbackDescription(axContext), nil
	}

	prompt := buildVisionPrompt(axContext)

	// Load and encode the screenshot for multimodal vision models.
	var msg inference.Message
	if imageData, err := os.ReadFile(screenshotPath); err == nil {
		msg = inference.Message{
			Role: "user",
			ContentBlocks: []inference.ContentBlock{
				{Type: "text", Text: prompt},
				{Type: "image", ImageData: base64.StdEncoding.EncodeToString(imageData), ImageType: "image/png"},
			},
		}
	} else {
		ve.log.Warn("failed to read screenshot, falling back to text-only", "path", screenshotPath, "error", err)
		msg = inference.Message{Role: "user", Content: prompt}
	}

	req := inference.ChatRequest{
		Model:       ve.model,
		Messages:    []inference.Message{msg},
		MaxTokens:   512,
		Temperature: 0.3,
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	tokens, errs := ve.provider.Chat(ctx, req)

	var response string
loop:
	for {
		select {
		case tok, ok := <-tokens:
			if !ok {
				break loop
			}
			response += tok.Text
		case err, ok := <-errs:
			if ok && err != nil {
				ve.log.Warn("vision engine LLM error, using fallback", "error", err)
				return ve.fallbackDescription(axContext), nil
			}
		case <-ctx.Done():
			return ve.fallbackDescription(axContext), nil
		}
	}

	desc, err := parseVisionResponse(response)
	if err != nil {
		ve.log.Debug("vision parse error, using fallback", "error", err)
		return ve.fallbackDescription(axContext), nil
	}
	return desc, nil
}

// extractJSONObject finds a balanced JSON object in a string and unmarshals
// it into v. Uses brace-depth counting to handle nested objects, which is
// necessary when LLM responses include nested JSON structures or extra text
// before/after the JSON payload.
func extractJSONObject(s string, v interface{}) error {
	start := indexOf(s, "{")
	if start < 0 {
		return fmt.Errorf("no JSON object found")
	}

	// Brace-depth counting: track { and } from the first opening brace.
	// Stop when depth returns to 0 (balanced object) or end of string.
	depth := 0
	end := -1
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
				goto parsed
			}
		case '"':
			// Skip over strings to avoid counting braces inside them.
			i++ // move past opening quote
			for i < len(s) {
				if s[i] == '\\' {
					i++ // skip escaped char
				} else if s[i] == '"' {
					break
				}
				i++
			}
		}
	}
parsed:
	if end < 0 || end <= start {
		// Fallback: use lastIndexOf for flat JSON (backward compatible).
		end = lastIndexOf(s, "}")
		if end <= start {
			return fmt.Errorf("no balanced JSON object found")
		}
	}
	return json.Unmarshal([]byte(s[start:end+1]), v)
}

func buildVisionPrompt(axContext string) string {
	prompt := `You are a screen reader. Describe what you see on screen in JSON format.

Return a JSON object with these fields:
- "active_app": the name of the foreground application
- "window_title": the title of the active window
- "elements": an array of UI elements, each with "role", "label", "position", "action"
- "summary": a 1-2 sentence description of what's on screen
- "is_actionable": true if there are interactive elements the user can click

Focus on actionable elements: buttons, text fields, links, menus, tabs.`

	if axContext != "" {
		prompt += fmt.Sprintf("\n\nAccessibility tree for context:\n%s", truncateStr(axContext, 4000))
	}

	return prompt
}

func parseVisionResponse(response string) (*ScreenDescription, error) {
	// Use robust brace-depth-aware extraction that handles nested JSON
	// and braces inside string literals.
	var desc ScreenDescription
	if err := extractJSONObject(response, &desc); err == nil && desc.Summary != "" {
		return &desc, nil
	}

	// Fallback: find the JSON region with simpler heuristics and retry.
	start := indexOf(response, "{")
	end := lastIndexOf(response, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found in vision response")
	}
	jsonStr := response[start : end+1]

	// Key-value extraction for malformed JSON.
	desc.ActiveApp = extractStrBetween(response, `"active_app"`, start, end)
	desc.WindowTitle = extractStrBetween(response, `"window_title"`, start, end)
	desc.Summary = extractStrBetween(response, `"summary"`, start, end)
	desc.IsActionable = contains(response[start:end+1], `"is_actionable": true`)

	// Try to parse elements array using brace-depth-aware extraction.
	if elStart := indexOf(jsonStr, `"elements"`); elStart >= 0 {
		if arrStart := indexOf(jsonStr[elStart:], "["); arrStart >= 0 {
			elementsJSON := jsonStr[elStart+arrStart:]
			if arrEnd := lastIndexOf(elementsJSON, "]"); arrEnd >= 0 {
				elementsJSON = elementsJSON[:arrEnd+1]
				var elements []UIElement
				if err := json.Unmarshal([]byte(elementsJSON), &elements); err == nil {
					desc.Elements = elements
				}
			}
		}
	}

	if desc.Summary == "" {
		desc.Summary = "Screen content (vision analysis unavailable)"
	}
	desc.Error = ""
	return &desc, nil
}

func (ve *VisionEngine) fallbackDescription(axContext string) *ScreenDescription {
	summary := "Screen content (vision analysis unavailable)"
	if axContext != "" {
		summary = fmt.Sprintf("Screen content from accessibility tree:\n%s", truncateStr(axContext, 500))
	}
	return &ScreenDescription{
		Summary:      summary,
		IsActionable: axContext != "",
	}
}

// ── Helpers ────────────────────────────────────────────────────────────

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func indexOf(s, substr string) int {
	for i := 0; i < len(s); i++ {
		if i+len(substr) <= len(s) && s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func lastIndexOf(s, substr string) int {
	for i := len(s) - len(substr); i >= 0; i-- {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func contains(s, substr string) bool {
	return indexOf(s, substr) >= 0
}

func extractStrBetween(s, key string, start, end int) string {
	// Find the key, then the colon, then the quoted string value.
	keyIdx := indexOf(s, key)
	if keyIdx < 0 || keyIdx > end {
		return ""
	}
	rest := s[keyIdx+len(key):]
	colonIdx := indexOf(rest, ":")
	if colonIdx < 0 {
		return ""
	}
	rest = rest[colonIdx+1:]
	// Skip whitespace.
	for len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n') {
		rest = rest[1:]
	}
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	rest = rest[1:]
	quoteIdx := indexOf(rest, "\"")
	if quoteIdx < 0 {
		return rest
	}
	return rest[:quoteIdx]
}
