package learning

// RPC provides Wails-bound learning engine methods.
//
// The engine is resolved through a getter rather than captured at construction
// time: Wails evaluates the Bind list before OnStartup runs, so a captured
// pointer would be nil for the whole process lifetime.
type LearningRPC struct {
	engineFn func() *Engine
}

// NewLearningRPC creates a learning RPC handler. engineFn is evaluated per call.
func NewLearningRPC(engineFn func() *Engine) *LearningRPC {
	return &LearningRPC{engineFn: engineFn}
}

// engine resolves the learning engine lazily.
func (r *LearningRPC) engine() *Engine {
	if r.engineFn == nil {
		return nil
	}
	return r.engineFn()
}

// GetPreferences returns learned user preferences.
func (r *LearningRPC) GetPreferences() []map[string]interface{} {
	e := r.engine()
	if e == nil {
		return []map[string]interface{}{}
	}
	prefs := e.Preferences()
	result := make([]map[string]interface{}, len(prefs))
	for i, p := range prefs {
		result[i] = map[string]interface{}{
			"key":        p.Key,
			"value":      p.Value,
			"confidence": p.Confidence,
		}
	}
	return result
}
