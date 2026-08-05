package desktop

import (
	"context"
	"fmt"
	"time"

	"github.com/simon/mneme/internal/tools"
)

// AutomationStep is a single step in an automation sequence.
type AutomationStep struct {
	Action string `json:"action"` // "click", "type", "key", "wait", "screenshot", "find"
	X      int    `json:"x,omitempty"`
	Y      int    `json:"y,omitempty"`
	Text   string `json:"text,omitempty"`
	Keys   string `json:"keys,omitempty"`
	Query  string `json:"query,omitempty"`   // for "find" action
	WaitMs int    `json:"wait_ms,omitempty"` // for "wait" action
}

// AutomationResult holds the outcome of an automation sequence.
type AutomationResult struct {
	StepsCompleted int      `json:"steps_completed"`
	TotalSteps     int      `json:"total_steps"`
	Errors         []string `json:"errors,omitempty"`
	Success        bool     `json:"success"`
}

// Automator runs sequences of automation steps using accessibility and
// computer control tools under the hood.
type Automator struct {
	ax       *AXInteract
	computer *tools.ComputerControl
	vision   *VisionClick
}

// NewAutomator creates an automator.
func NewAutomator() *Automator {
	return &Automator{
		ax:       NewAXInteract(),
		computer: tools.NewComputerControl(),
		vision:   NewVisionClick(nil), // vision model injected later
	}
}

// WithVision sets the vision model for vision-based actions.
func (a *Automator) WithVision(visionFunc func(imagePath, target string) (int, int, error)) *Automator {
	a.vision = NewVisionClick(visionFunc)
	return a
}

// Run executes a sequence of automation steps in order.
// Applies safety checks: coordinate clamping, sensitive-app denylist,
// settle-wait after state-changing actions, and no-progress guard.
func (a *Automator) Run(ctx context.Context, steps []AutomationStep) *AutomationResult {
	result := &AutomationResult{TotalSteps: len(steps)}
	var consecutiveErrors int

	for i, step := range steps {
		if ctx.Err() != nil {
			result.Errors = append(result.Errors, "cancelled")
			break
		}

		// No-progress guard: abort after 3 consecutive identical errors.
		if consecutiveErrors >= 3 {
			result.Errors = append(result.Errors, "automation aborted: 3 consecutive failures")
			break
		}

		// Clamp click coordinates for safety at each mouse step.
		step.X, step.Y = clampCoord(step.X, step.Y)

		// Pre-check: reject interaction with sensitive apps for ALL actions,
		// not just find/vision_click (matches Rust security-first approach).
		if sensitive, name := IsSensitiveApp(step.Query); sensitive {
			err := fmt.Errorf("refusing to interact with sensitive application: %q matches denylist entry %q", step.Query, name)
			result.Errors = append(result.Errors, fmt.Sprintf("step %d (%s): %v", i+1, step.Action, err))
			consecutiveErrors++
			continue
		}
		// Also check the frontmost app.
		if appName := frontmostAppName(); appName != "" {
			if sensitive, name := IsSensitiveApp(appName); sensitive {
				err := fmt.Errorf("refusing to automate: frontmost app %q matches denylist entry %q", appName, name)
				result.Errors = append(result.Errors, fmt.Sprintf("step %d (%s): %v", i+1, step.Action, err))
				consecutiveErrors++
				continue
			}
		}

		var err error
		stateChanging := false
		switch step.Action {
		case "click":
			stateChanging = true
			r := a.computer.Execute(ctx, map[string]interface{}{"action": "mouse_click", "x": float64(step.X), "y": float64(step.Y)})
			if r.Error != "" {
				err = fmt.Errorf("%s", r.Error)
			}
		case "right_click":
			stateChanging = true
			r := a.computer.Execute(ctx, map[string]interface{}{"action": "mouse_right_click", "x": float64(step.X), "y": float64(step.Y)})
			if r.Error != "" {
				err = fmt.Errorf("%s", r.Error)
			}
		case "double_click":
			stateChanging = true
			r := a.computer.Execute(ctx, map[string]interface{}{"action": "mouse_double_click", "x": float64(step.X), "y": float64(step.Y)})
			if r.Error != "" {
				err = fmt.Errorf("%s", r.Error)
			}
		case "type":
			stateChanging = true
			r := a.computer.Execute(ctx, map[string]interface{}{"action": "type_text", "text": step.Text})
			if r.Error != "" {
				err = fmt.Errorf("%s", r.Error)
			}
		case "key":
			stateChanging = true
			r := a.computer.Execute(ctx, map[string]interface{}{"action": "key_press", "keys": step.Keys})
			if r.Error != "" {
				err = fmt.Errorf("%s", r.Error)
			}
		case "key_combo":
			stateChanging = true
			r := a.computer.Execute(ctx, map[string]interface{}{"action": "key_combo", "keys": step.Keys})
			if r.Error != "" {
				err = fmt.Errorf("%s", r.Error)
			}
		case "wait":
			if step.WaitMs > 0 {
				select {
				case <-ctx.Done():
					err = ctx.Err()
				case <-time.After(time.Duration(step.WaitMs) * time.Millisecond):
				}
			}
		case "find":
			ring, findErr := a.ax.FindElement(step.Query)
			if findErr != nil {
				err = findErr
			} else if ring.Error != "" {
				err = fmt.Errorf("%s", ring.Error)
			}
		case "vision_click":
			stateChanging = true
			if a.vision != nil {
				err = a.vision.ClickByDescription(ctx, step.Query)
			} else {
				err = fmt.Errorf("vision_click: no vision model configured")
			}
		default:
			err = fmt.Errorf("unknown action: %s", step.Action)
		}

		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("step %d (%s): %v", i+1, step.Action, err))
			consecutiveErrors++
		} else {
			result.StepsCompleted++
			consecutiveErrors = 0
			// After state-changing actions, wait for UI to settle instead of
			// using fixed sleeps. Falls back to 500ms if AX is unavailable.
			if stateChanging {
				SettleWait(ctx, a.ax, 500*time.Millisecond)
			}
		}
	}

	result.Success = len(result.Errors) == 0
	return result
}
