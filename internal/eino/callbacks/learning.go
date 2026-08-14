package callbacks

import (
	"context"

	"github.com/simon/mneme/internal/learning"
)

// LearningCallback wraps the learning.Engine to trigger post-turn
// reflection from the eino pipeline. All methods are nil-safe.
type LearningCallback struct {
	engine *learning.Engine
}

// NewLearningCallback creates a LearningCallback. Passing nil for
// engine is allowed; methods will simply no-op.
func NewLearningCallback(engine *learning.Engine) *LearningCallback {
	return &LearningCallback{engine: engine}
}

// OnTurnEnd triggers post-turn preference extraction by calling
// Reflect on the learning engine. The extracted preferences are
// automatically stored by the engine.
func (l *LearningCallback) OnTurnEnd(ctx context.Context, threadID, userMsg, assistantMsg string) {
	if l.engine == nil {
		return
	}
	l.engine.Reflect(ctx, threadID, userMsg, assistantMsg)
}
