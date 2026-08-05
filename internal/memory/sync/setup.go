package sync

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"
)

// RegisterEnvConnectors reads well-known environment variables and registers
// the corresponding data source connectors into the manager. Call once during
// app startup assembly.
//
// Supported env vars:
//   - TWITTER_BEARER_TOKEN (+ TWITTER_USER_ID) → Twitter timeline sync
func RegisterEnvConnectors(mgr *Manager, log *slog.Logger) {
	if mgr == nil {
		return
	}
	if token := os.Getenv("TWITTER_BEARER_TOKEN"); token != "" {
		userID := os.Getenv("TWITTER_USER_ID")
		mgr.Register(NewTwitterConnector(userID, "", token, 100))
		if log != nil {
			log.Info("sync: registered Twitter connector", "user_id", userID)
		}
	}
}

// SyncRunner returns a no-arg function suitable for cron.Handler that runs
// all registered connectors through the given pipeline.
func SyncRunner(mgr *Manager, pipeline Pipeline) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		mgr.SyncAll(ctx, pipeline)
		return nil
	}
}

// SnapshotFunc is called after each sync tick to capture a memory snapshot.
type SnapshotFunc func(label string) error

// SyncRunnerWithSnapshot returns a cron handler that runs all connectors and
// automatically takes a memory diff snapshot after each sync tick, matching
// the Rust auto-snapshot behaviour after Composio/Workspace/MCP pipelines.
func SyncRunnerWithSnapshot(mgr *Manager, pipeline Pipeline, snapshot SnapshotFunc) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		mgr.SyncAll(ctx, pipeline)
		if snapshot != nil {
			label := fmt.Sprintf("auto-%s", time.Now().UTC().Format("2006-01-02T15:04"))
			if err := snapshot(label); err != nil {
				slog.Warn("sync: auto-snapshot failed", "error", err)
			}
		}
		return nil
	}
}
