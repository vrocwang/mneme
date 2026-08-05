package cron

import "context"

// ChatSender sends a message to the AI agent and returns the response.
type ChatSender func(ctx context.Context, prompt string) (string, error)

// RPC provides Wails-bound cron scheduler methods.
type CronRPC struct {
	sched  *Scheduler
	sendFn ChatSender // optional: enables agent-type cron jobs
}

// NewRPC creates a cron RPC handler.
func NewCronRPC(sched *Scheduler) *CronRPC {
	return &CronRPC{sched: sched}
}

// WithChatSender sets the function used to execute agent-type cron jobs.
func (r *CronRPC) WithChatSender(fn ChatSender) *CronRPC {
	r.sendFn = fn
	return r
}

// GetCronJobs returns all scheduled jobs.
func (r *CronRPC) GetCronJobs() []map[string]interface{} {
	if r.sched == nil {
		return []map[string]interface{}{}
	}
	jobs := r.sched.List()
	result := make([]map[string]interface{}, len(jobs))
	for i, j := range jobs {
		result[i] = map[string]interface{}{
			"id":       j.ID,
			"name":     j.Name,
			"schedule": j.Schedule,
			"enabled":  j.Enabled,
		}
	}
	return result
}

// ToggleCronJob enables or disables a cron job by ID. Returns the new state.
func (r *CronRPC) ToggleCronJob(id string, enabled bool) map[string]interface{} {
	if r.sched == nil {
		return map[string]interface{}{"ok": false, "error": "scheduler not available"}
	}
	for _, j := range r.sched.List() {
		if j.ID == id {
			j.Enabled = enabled
			if s := r.sched.Store(); s != nil {
				s.SaveJob(j)
			}
			return map[string]interface{}{"ok": true, "id": id, "enabled": enabled}
		}
	}
	return map[string]interface{}{"ok": false, "error": "job not found"}
}

// TriggerCronJob runs a cron job immediately by ID.
func (r *CronRPC) TriggerCronJob(id string) map[string]interface{} {
	if r.sched == nil {
		return map[string]interface{}{"ok": false, "error": "scheduler not available"}
	}
	if err := r.sched.Run(id); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "id": id}
}

// AddCronJob adds a new user-defined cron job. Agent-type jobs use the
// configured ChatSender to invoke the AI agent with the given prompt.
func (r *CronRPC) AddCronJob(name, schedule, prompt string) map[string]interface{} {
	if r.sched == nil {
		return map[string]interface{}{"ok": false, "error": "scheduler not available"}
	}
	if name == "" || schedule == "" {
		return map[string]interface{}{"ok": false, "error": "name and schedule are required"}
	}
	if prompt != "" && r.sendFn == nil {
		return map[string]interface{}{"ok": false, "error": "agent-type cron jobs require a configured AI provider"}
	}
	job := &Job{
		Name:        name,
		Schedule:    schedule,
		Enabled:     true,
		JobType:     JobTypeAgent,
		AgentPrompt: prompt,
	}
	if prompt != "" && r.sendFn != nil {
		sendFn := r.sendFn
		job.Handler = func(ctx context.Context) error {
			_, err := sendFn(ctx, prompt)
			return err
		}
	}
	r.sched.Add(job)
	return map[string]interface{}{"ok": true, "id": job.ID, "name": name, "schedule": schedule, "enabled": true}
}

// RemoveCronJob removes a cron job by ID. Refuses to remove builtin jobs.
func (r *CronRPC) RemoveCronJob(id string) map[string]interface{} {
	if r.sched == nil {
		return map[string]interface{}{"ok": false, "error": "scheduler not available"}
	}
	r.sched.Remove(id)
	return map[string]interface{}{"ok": true, "id": id}
}
