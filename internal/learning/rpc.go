package learning

// RPC provides Wails-bound learning engine methods.
type LearningRPC struct {
	engine *Engine
}

// NewRPC creates a learning RPC handler.
func NewLearningRPC(engine *Engine) *LearningRPC {
	return &LearningRPC{engine: engine}
}

// GetPreferences returns learned user preferences.
func (r *LearningRPC) GetPreferences() []map[string]interface{} {
	if r.engine == nil {
		return []map[string]interface{}{}
	}
	prefs := r.engine.Preferences()
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
