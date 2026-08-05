package tools

import (
	"context"
	"fmt"
	"time"
)

// NewCurrentTime creates a tool that returns the current date and time.
func NewCurrentTime() Tool {
	return &currentTimeTool{
		BaseTool: BaseTool{
			SchemaVal: Schema{
				Name:        "current_time",
				Description: "Returns the current date and time in ISO 8601 format. Use this when you need to know the current time for scheduling, time-based logic, or date calculations.",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
			PermLevel:      PermNone,
			HasSideEffects: false,
			MaxOutputChars: 100,
			ToolCategory:   CategorySystem,
		},
	}
}

type currentTimeTool struct{ BaseTool }

func (t *currentTimeTool) Execute(ctx context.Context, args map[string]interface{}) Result {
	now := time.Now()
	return Result{
		Success: true,
		Output: fmt.Sprintf("Current time: %s (Unix: %d)\nDay: %s\nWeek: %s",
			now.Format(time.RFC3339),
			now.Unix(),
			now.Weekday().String(),
			now.Format("2006-W02"),
		),
	}
}

// NewAskUser creates a tool for the agent to ask the user a question.
// The result is surfaced through the approval/interaction system.
func NewAskUser() Tool {
	return &askUserTool{
		BaseTool: BaseTool{
			SchemaVal: Schema{
				Name:        "ask_user",
				Description: "Ask the user a clarifying question. The turn pauses until the user responds. Use this only when you genuinely need user input — prefer making your best attempt first.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"question": map[string]interface{}{
							"type":        "string",
							"description": "The question to ask the user.",
						},
						"options": map[string]interface{}{
							"type":        "array",
							"description": "Optional list of choices for the user (max 5).",
							"items": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"required": []string{"question"},
				},
			},
			PermLevel:      PermNone,
			HasSideEffects: true,
			MaxOutputChars: 1000,
			ToolCategory:   CategoryInteraction,
		},
	}
}

type askUserTool struct{ BaseTool }

// ASK_USER_MARKER is returned in tool results when the agent asks the user a question.
// The caller can detect this marker to pause the turn and surface the question.
const ASK_USER_MARKER = "__ASK_USER__"

func (t *askUserTool) Execute(ctx context.Context, args map[string]interface{}) Result {
	question, _ := args["question"].(string)
	if question == "" {
		return Result{Error: "question is required"}
	}

	var options []string
	if opts, ok := args["options"].([]interface{}); ok {
		for _, o := range opts {
			if s, ok := o.(string); ok && len(options) < 5 {
				options = append(options, s)
			}
		}
	}

	out := ASK_USER_MARKER + "\n" + question
	if len(options) > 0 {
		out += "\nOptions:"
		for i, o := range options {
			out += fmt.Sprintf("\n  %d. %s", i+1, o)
		}
	}
	return Result{Success: true, Output: out}
}

// NewWait creates a tool for pausing between dependent operations.
func NewWait() Tool {
	return &waitTool{
		BaseTool: BaseTool{
			SchemaVal: Schema{
				Name:        "wait",
				Description: "Pause execution for a specified number of seconds. Use sparingly between dependent operations.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"seconds": map[string]interface{}{
							"type":        "number",
							"description": "Seconds to wait (max: 30)",
						},
					},
					"required": []string{"seconds"},
				},
			},
			PermLevel:      PermNone,
			HasSideEffects: false,
			MaxOutputChars: 200,
			ToolCategory:   CategorySystem,
		},
	}
}

type waitTool struct{ BaseTool }

func (t *waitTool) Execute(ctx context.Context, args map[string]interface{}) Result {
	secs := 1.0
	if s, ok := args["seconds"].(float64); ok {
		secs = s
	}
	if secs > 30 {
		secs = 30
	}
	if secs < 0 {
		secs = 0
	}

	timer := time.NewTimer(time.Duration(secs * float64(time.Second)))
	select {
	case <-timer.C:
		return Result{Success: true, Output: fmt.Sprintf("Waited %.1f seconds", secs)}
	case <-ctx.Done():
		timer.Stop()
		return Result{Error: "wait cancelled"}
	}
}
