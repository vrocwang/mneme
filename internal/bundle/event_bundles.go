package bundle

import (
	"context"
	"time"

	"github.com/simon/mneme/internal/agent"
	"github.com/simon/mneme/internal/cron"
	"github.com/simon/mneme/internal/health"
	"github.com/simon/mneme/internal/learning"
	memsync "github.com/simon/mneme/internal/memory/sync"
	"github.com/simon/mneme/internal/notifications"
	"github.com/simon/mneme/internal/subconscious"
	"github.com/simon/mneme/pkg/dispose"
)

// EventBundles returns the event-driven behavior bundles in registration
// order. These attach listeners to the agent loop, the cron scheduler, the
// subconscious engine, and the heartbeat — none of which is a privileged
// core behavior; each can be disabled independently.
func EventBundles() []Bundle {
	return []Bundle{
		eventHooksBundle(),
		cronJobsBundle(),
		subconsciousBundle(),
		heartbeatBundle(),
	}
}

// eventHooksBundle registers the five post-turn hooks (persistence, metrics,
// tool tracking, cost tracking, session memory).
func eventHooksBundle() Bundle {
	return Func("event-hooks", func(ctx context.Context, d *Deps) (dispose.Func, error) {
		if d.HookReg == nil {
			return nil, nil
		}

		if d.ConvStore != nil {
			s := d.ConvStore
			d.HookReg.Register(agent.NewPostTurnHook("persistence", func(ctx context.Context, snap *agent.TurnSnapshot) {
				if snap == nil {
					return
				}
				_ = s.AddMessage(snap.ThreadID, "assistant", snap.Response)
			}))
		}
		if d.Metrics != nil {
			turnCtr, turnDur, errCtr := health.DefaultAgentMetrics(d.Metrics)
			toolCtr := d.Metrics.Counter("tool_calls_total")
			toolFailCtr := d.Metrics.Counter("tool_failures_total")
			d.HookReg.Register(agent.NewPostTurnHook("metrics", func(ctx context.Context, snap *agent.TurnSnapshot) {
				turnCtr.Inc()
				turnDur.Observe(snap.Duration.Milliseconds())
				if snap.Error != nil {
					errCtr.Inc()
				}
				toolCtr.Add(int64(len(snap.ToolCalls)))
				for _, tc := range snap.ToolCalls {
					if tc.Error != "" {
						toolFailCtr.Inc()
					}
				}
			}))
		}
		if d.ToolTracker != nil {
			tt := d.ToolTracker
			d.HookReg.Register(agent.NewPostTurnHook("tool-tracker", func(ctx context.Context, snap *agent.TurnSnapshot) {
				for _, tc := range snap.ToolCalls {
					dur := float64(0)
					if tc.Duration > 0 {
						dur = float64(tc.Duration.Milliseconds())
					}
					tt.RecordCall(tc.Name, tc.Error == "", dur, tc.Error)
				}
			}))
		}
		if d.CostTracker != nil {
			d.HookReg.Register(agent.NewCostTrackingHook(d.CostTracker))
		}
		if d.SessionMemory != nil {
			sm := d.SessionMemory
			d.HookReg.Register(agent.NewPostTurnHook("session-memory", func(ctx context.Context, snap *agent.TurnSnapshot) {
				sm.TickTurn()
			}))
		}
		return nil, nil
	})
}

// cronJobsBundle registers the three hourly background jobs: memory
// maintenance, transcript ingestion, and memory-sync connectors.
func cronJobsBundle() Bundle {
	return Func("cron-jobs", func(ctx context.Context, d *Deps) (dispose.Func, error) {
		if d.Cron == nil {
			return nil, nil
		}

		if d.Pipeline != nil {
			d.Cron.Add(&cron.Job{
				ID:       "memory-maintenance",
				Name:     "Memory pipeline maintenance",
				Schedule: "hourly",
				Enabled:  true,
				Handler: func(ctx context.Context) error {
					return d.Pipeline.IndexContent("cron", "hourly maintenance pulse")
				},
			})
		}

		if d.ConvStore != nil && d.Learning != nil {
			ingestor := learning.NewTranscriptIngestor(d.ConvStore, learning.GlobalBuffer(), d.Log.Info)
			d.Cron.Add(&cron.Job{
				ID:       "transcript-ingest",
				Name:     "Mine past transcripts for learning signals",
				Schedule: "hourly",
				Enabled:  true,
				Handler: func(ctx context.Context) error {
					return ingestor.Ingest(ctx)
				},
			})
		}

		if d.SyncMgr != nil && d.Pipeline != nil {
			d.Cron.Add(&cron.Job{
				ID:       "sync-connectors",
				Name:     "Memory sync connectors",
				Schedule: "hourly",
				Enabled:  true,
				Handler: memsync.SyncRunnerWithSnapshot(d.SyncMgr, d.Pipeline,
					func(label string) error {
						if diff := d.Pipeline.DiffService(); diff != nil {
							_, err := diff.CreateCheckpoint(label)
							return err
						}
						return nil
					}),
			})
		}
		return nil, nil
	})
}

// subconsciousBundle registers the four subconscious evaluators (memory gap,
// conversation digest, idle reminder, and the optional LLM evaluator).
func subconsciousBundle() Bundle {
	return Func("subconscious", func(ctx context.Context, d *Deps) (dispose.Func, error) {
		if d.Subcon == nil {
			return nil, nil
		}

		gapEval := subconscious.NewMemoryGapEvaluator(d.Log)
		if d.Pipeline != nil {
			gapEval = gapEval.WithPipeline(subconscious.NewMemoryPipelineAdapter(d.Pipeline))
		}
		d.Subcon.Register(gapEval)

		digestEval := subconscious.NewConversationDigestEvaluator(d.Log)
		if d.Pipeline != nil {
			digestEval = digestEval.WithPipeline(subconscious.NewMemoryPipelineAdapter(d.Pipeline))
		}
		d.Subcon.Register(digestEval)

		d.Subcon.Register(subconscious.NewIdleReminderEvaluator(d.Log))

		if d.Provider != nil && d.Pipeline != nil && d.Cfg != nil {
			llmEval := subconscious.NewLLMEvaluator(d.Log, d.Provider, d.Cfg.Agent.DefaultModel,
				subconscious.NewMemoryPipelineAdapter(d.Pipeline)).
				WithWorkspace(d.Workspace)
			if d.Learning != nil {
				llmEval = llmEval.WithPrefs(func() []subconscious.PreferencePair {
					prefs := d.Learning.Preferences()
					out := make([]subconscious.PreferencePair, len(prefs))
					for i, p := range prefs {
						out[i] = subconscious.PreferencePair{Key: p.Key, Value: p.Value, Confidence: p.Confidence}
					}
					return out
				})
			}
			d.Subcon.Register(llmEval)
		}
		return nil, nil
	})
}

// heartbeatBundle registers the heartbeat handler that runs the subconscious
// engine and routes its actions to the notification bus.
func heartbeatBundle() Bundle {
	return Func("heartbeat", func(ctx context.Context, d *Deps) (dispose.Func, error) {
		if d.Heartbeat == nil || d.Subcon == nil {
			return nil, nil
		}
		d.Heartbeat.Register(func(ctx context.Context) {
			actions := d.Subcon.Think(ctx)
			d.Subcon.HandleActions(actions, func(action subconscious.Action) {
				if d.NotifBus == nil {
					return
				}
				switch action.Type {
				case "suggestion":
					d.NotifBus.Notify(notifications.KindSystemAlert, action.Title, action.Message, "", "")
				case "nudge":
					d.NotifBus.Notify(notifications.KindReminder, action.Title, action.Message, "", "")
				default:
					d.NotifBus.Notify(notifications.KindSystemAlert, action.Title, action.Message, "", "")
				}
			})
		})
		return nil, nil
	})
}

// HeartbeatInterval is the default heartbeat cadence used by the boot layer
// when constructing the heartbeat engine. Exported so the cadence stays in a
// single place rather than being hardcoded in boot.
const HeartbeatInterval = 30 * time.Second
