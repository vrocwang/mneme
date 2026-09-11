package webhooks

// WebhookRPC provides Wails-bound methods for webhook tunnel management.
//
// The tunnel manager is resolved through a getter rather than captured at
// construction time: Wails evaluates the Bind slice before OnStartup fires, so
// a captured pointer would be nil for the whole process lifetime. Methods still
// guard against a nil manager so the UI degrades gracefully when the webhook
// server is disabled.
type WebhookRPC struct {
	tmFn func() *TunnelManager
}

// NewWebhookRPC creates a webhook RPC handler. tmFn is evaluated per call.
func NewWebhookRPC(tmFn func() *TunnelManager) *WebhookRPC {
	return &WebhookRPC{tmFn: tmFn}
}

// tm resolves the tunnel manager lazily.
func (r *WebhookRPC) tm() *TunnelManager {
	if r.tmFn == nil {
		return nil
	}
	return r.tmFn()
}

func (r *WebhookRPC) ok(data map[string]interface{}) map[string]interface{} {
	if data == nil {
		data = map[string]interface{}{}
	}
	data["ok"] = true
	return data
}

func (r *WebhookRPC) err(msg string) map[string]interface{} {
	return map[string]interface{}{"ok": false, "error": msg}
}

func (r *WebhookRPC) ListTunnels() map[string]interface{} {
	tm := r.tm()
	if tm == nil {
		return r.ok(map[string]interface{}{"tunnels": []map[string]interface{}{}})
	}
	tunnels := tm.ListTunnels()
	out := make([]map[string]interface{}, 0, len(tunnels))
	for _, t := range tunnels {
		out = append(out, map[string]interface{}{
			"id":          t.ID,
			"tunnel_uuid": t.TunnelUUID,
			"target":      string(t.Target),
			"target_id":   t.TargetID,
			"description": t.Description,
			"enabled":     t.Enabled,
			"created_at":  t.CreatedAt,
		})
	}
	return r.ok(map[string]interface{}{"tunnels": out})
}

func (r *WebhookRPC) CreateTunnel(target, targetID, description string) map[string]interface{} {
	tm := r.tm()
	if tm == nil {
		return r.err("tunnel manager not initialized")
	}
	t, err := tm.RegisterTunnel(TunnelTarget(target), targetID, description)
	if err != nil {
		return r.err(err.Error())
	}
	return r.ok(map[string]interface{}{"tunnel": map[string]interface{}{
		"id": t.ID, "tunnel_uuid": t.TunnelUUID, "target": string(t.Target),
		"target_id": t.TargetID, "description": t.Description,
		"enabled": t.Enabled, "created_at": t.CreatedAt,
	}})
}

func (r *WebhookRPC) GetTunnel(uuid string) map[string]interface{} {
	tm := r.tm()
	if tm == nil {
		return r.err("tunnel manager not initialized")
	}
	t, err := tm.GetTunnel(uuid)
	if err != nil {
		return r.err(err.Error())
	}
	return r.ok(map[string]interface{}{"tunnel": map[string]interface{}{
		"id": t.ID, "target": string(t.Target), "target_id": t.TargetID,
		"description": t.Description, "enabled": t.Enabled, "created_at": t.CreatedAt,
	}})
}

func (r *WebhookRPC) UpdateTunnel(uuid string, enabled *bool, description *string) map[string]interface{} {
	tm := r.tm()
	if tm == nil {
		return r.err("tunnel manager not initialized")
	}
	if err := tm.UpdateTunnel(uuid, enabled, description); err != nil {
		return r.err(err.Error())
	}
	return r.ok(nil)
}

func (r *WebhookRPC) DeleteTunnel(uuid string) map[string]interface{} {
	tm := r.tm()
	if tm == nil {
		return r.err("tunnel manager not initialized")
	}
	if err := tm.DeleteTunnel(uuid); err != nil {
		return r.err(err.Error())
	}
	return r.ok(nil)
}

func (r *WebhookRPC) GetTunnelBandwidth(uuid string) map[string]interface{} {
	tm := r.tm()
	if tm == nil {
		return r.ok(map[string]interface{}{"bandwidth": 0})
	}
	return r.ok(map[string]interface{}{"bandwidth": tm.GetBandwidth(uuid)})
}

func (r *WebhookRPC) ListTunnelActivity(limit int) map[string]interface{} {
	tm := r.tm()
	if tm == nil {
		return r.ok(map[string]interface{}{"activities": []map[string]interface{}{}})
	}
	activities := tm.ListActivities(limit)
	out := make([]map[string]interface{}, 0, len(activities))
	for _, a := range activities {
		out = append(out, map[string]interface{}{
			"id": a.ID, "tunnel_uuid": a.TunnelUUID, "request_id": a.RequestID,
			"status": a.Status, "response_size": a.ResponseSize,
			"duration_ms": a.Duration, "error": a.Error, "created_at": a.CreatedAt,
		})
	}
	return r.ok(map[string]interface{}{"activities": out})
}

func (r *WebhookRPC) ClearTunnelActivity() map[string]interface{} {
	tm := r.tm()
	if tm == nil {
		return r.ok(nil)
	}
	tm.ClearActivities()
	return r.ok(nil)
}

func (r *WebhookRPC) TunnelCount() int {
	tm := r.tm()
	if tm == nil {
		return 0
	}
	return len(tm.ListTunnels())
}
