// Package migrations provides versioned startup data migrations gated by Config.SchemaVersion.
// Each migration is idempotent, best-effort, and non-blocking on failure (errors are logged, not fatal).
//
// Current schema version tracks the latest applied migration. New migrations are only run when
// the config's schema_version is higher than the last applied version.
package migrations

import (
	"database/sql"
	"fmt"
	"log/slog"
)

// CurrentSchemaVersion is the target schema version. Bump this when adding new migrations.
const CurrentSchemaVersion = 6

// Migration is a single idempotent data migration.
type Migration struct {
	Version int
	Name    string
	Apply   func(db *sql.DB, log *slog.Logger) error
}

// All returns all startup migrations in version order.
func All() []Migration {
	return []Migration{
		{Version: 1, Name: "phase_out_profile_md", Apply: phaseOutProfileMD},
		{Version: 2, Name: "unify_ai_provider_settings", Apply: unifyAIProviderSettings},
		{Version: 3, Name: "retire_chat_v1_model", Apply: retireChatV1Model},
		{Version: 4, Name: "expand_autonomy_defaults", Apply: expandAutonomyDefaults},
		{Version: 5, Name: "remove_write_auto_approve", Apply: removeWriteAutoApprove},
		{Version: 6, Name: "repair_http_request_limits", Apply: repairHTTPRequestLimits},
	}
}

// Run applies any pending startup migrations whose version > lastAppliedVersion.
// Each migration runs in its own transaction. Failures are logged and the runner continues.
func Run(db *sql.DB, log *slog.Logger, lastAppliedVersion int) error {
	if db == nil {
		return nil
	}

	if err := ensureMetaTable(db); err != nil {
		return fmt.Errorf("migrations: ensure meta table: %w", err)
	}

	for _, m := range All() {
		if m.Version <= lastAppliedVersion {
			continue
		}
		if isApplied(db, m.Version) {
			continue
		}

		log.Info("running startup migration", "version", m.Version, "name", m.Name)

		if err := m.Apply(db, log); err != nil {
			log.Warn("migration failed — skipping", "version", m.Version, "name", m.Name, "error", err)
			continue
		}

		// Record the applied version. This is in its own lightweight transaction.
		// Migrations are idempotent by design (INSERT OR IGNORE, table-existence
		// guards), so a crash between Apply and version recording is safe —
		// the migration will simply be re-applied on next startup.
		tx, err := db.Begin()
		if err != nil {
			log.Warn("migration record tx failed — skipping", "version", m.Version, "name", m.Name, "error", err)
			continue
		}
		if _, err := tx.Exec("INSERT INTO schema_migrations (version, name) VALUES (?, ?)", m.Version, m.Name); err != nil {
			tx.Rollback()
			log.Warn("migration record failed — skipping", "version", m.Version, "name", m.Name, "error", err)
			continue
		}
		if err := tx.Commit(); err != nil {
			log.Warn("migration commit failed — skipping", "version", m.Version, "name", m.Name, "error", err)
			continue
		}

		log.Info("startup migration applied", "version", m.Version, "name", m.Name)
	}
	return nil
}

// ── Internals ──────────────────────────────────────────────────────────

func ensureMetaTable(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`)
	return err
}

func isApplied(db *sql.DB, version int) bool {
	var count int
	db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version).Scan(&count)
	return count > 0
}

// ── Migration v1: Phase out deprecated profile.md files ──────────────

func phaseOutProfileMD(db *sql.DB, log *slog.Logger) error {
	// Ensure memory_chunks table exists before trying to update.
	// The profile.md pattern stored user preferences as markdown files;
	// this migration marks them as deprecated in the store.
	_, err := db.Exec(`UPDATE memory_chunks SET source = 'deprecated_profile_md' WHERE source = 'profile.md'`)
	if err != nil {
		// Table may not exist yet — that's fine, no data to migrate.
		log.Debug("phase_out_profile_md: memory_chunks table not present — skipping", "error", err)
		return nil
	}
	return nil
}

// ── Migration v2: Unify AI provider settings ─────────────────────────

func unifyAIProviderSettings(db *sql.DB, log *slog.Logger) error {
	// Ensure KV store has the unified provider namespace.
	_, err := db.Exec(`
		INSERT OR IGNORE INTO kv_global (key, value) VALUES
			('ai.default_provider', 'auto'),
			('ai.default_model', '')
	`)
	if err != nil {
		log.Debug("unify_ai_provider_settings: kv_global table not present — skipping", "error", err)
		return nil
	}
	return nil
}

// ── Migration v3: Retire chat v1 model preference ────────────────────

func retireChatV1Model(db *sql.DB, log *slog.Logger) error {
	// Remove deprecated chat_v1 model entries from the KV store.
	_, err := db.Exec(`DELETE FROM kv_global WHERE key = 'ai.chat_v1_model'`)
	if err != nil {
		log.Debug("retire_chat_v1_model: kv_global table not present — skipping", "error", err)
		return nil
	}
	return nil
}

// ── Migration v4: Expand autonomy defaults ───────────────────────────

func expandAutonomyDefaults(db *sql.DB, log *slog.Logger) error {
	// Set default autonomy level if not already configured.
	_, err := db.Exec(`
		INSERT OR IGNORE INTO kv_global (key, value) VALUES
			('autonomy.level', 'supervised'),
			('autonomy.workspace_only', 'true')
	`)
	if err != nil {
		log.Debug("expand_autonomy_defaults: kv_global table not present — skipping", "error", err)
		return nil
	}
	return nil
}

// ── Migration v5: Remove write auto-approve ──────────────────────────

func removeWriteAutoApprove(db *sql.DB, log *slog.Logger) error {
	// Remove deprecated write auto-approve entries from the allowlist.
	_, err := db.Exec(`DELETE FROM approval_allowlist WHERE tool_name IN ('write_file', 'edit_file')`)
	if err != nil {
		log.Debug("remove_write_auto_approve: approval_allowlist table not present — skipping", "error", err)
		return nil
	}
	return nil
}

// ── Migration v6: Repair HTTP request limits ─────────────────────────

func repairHTTPRequestLimits(db *sql.DB, log *slog.Logger) error {
	// Set safe defaults for HTTP request limits if they don't exist.
	_, err := db.Exec(`
		INSERT OR IGNORE INTO kv_global (key, value) VALUES
			('http.max_body_bytes', '1048576'),
			('http.max_timeout_secs', '30')
	`)
	if err != nil {
		log.Debug("repair_http_request_limits: kv_global table not present — skipping", "error", err)
		return nil
	}

	// Reconcile orphaned providers — remove providers referencing deleted models.
	_, err = db.Exec(`
		DELETE FROM kv_namespace WHERE
			namespace = 'ai_providers' AND
			key LIKE '%.model' AND
			value = ''
	`)
	if err != nil {
		log.Debug("repair_http_request_limits: reconcile orphaned providers — skipping", "error", err)
		return nil
	}
	return nil
}
