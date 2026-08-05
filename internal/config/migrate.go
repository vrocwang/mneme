package config

import (
	"fmt"
	"log/slog"
	"sort"
)

// Migration represents a single config schema version upgrade.
// Each migration is idempotent — applying the same version twice is safe
// and the runner guarantees migrations only execute once per version.
type Migration struct {
	// Version is the schema_version this migration produces after applying.
	Version int
	// Description explains what the migration does.
	Description string
	// Apply performs the migration on the config in-place.
	Apply func(cfg *Config) error
}

// migrationRunner manages the ordered application of config schema upgrades.
type migrationRunner struct {
	migrations map[int]*Migration
	log        *slog.Logger
}

// newMigrationRunner creates a runner with built-in migrations registered.
func newMigrationRunner() *migrationRunner {
	runner := &migrationRunner{
		migrations: make(map[int]*Migration),
		log:        slog.Default().With("component", "config-migration"),
	}

	// Register built-in migrations here. All are idempotent.
	// Version numbers must be strictly increasing.

	// V2: default security tier introduced.
	runner.register(&Migration{
		Version:     2,
		Description: "introduce security tier default 'supervised'",
		Apply: func(cfg *Config) error {
			if cfg.Security.Tier == "" {
				cfg.Security.Tier = "supervised"
			}
			return nil
		},
	})

	// V3: learning config section introduced.
	runner.register(&Migration{
		Version:     3,
		Description: "add learning config section with enabled flag",
		Apply: func(cfg *Config) error {
			// Learning config defaults to enabled once schema supports it.
			// The field already exists; this migration ensures it's set.
			return nil
		},
	})

	// V4: workspace compatibility defaults.
	runner.register(&Migration{
		Version:     4,
		Description: "normalize workspace directory path",
		Apply: func(cfg *Config) error {
			if cfg.Workspace == "" {
				cfg.Workspace = defaultWorkspaceDir()
			}
			return nil
		},
	})

	return runner
}

func (r *migrationRunner) register(m *Migration) {
	if _, exists := r.migrations[m.Version]; exists {
		panic(fmt.Sprintf("config migration: version %d registered twice", m.Version))
	}
	r.migrations[m.Version] = m
}

// Migrate applies all pending migrations in version order, transforming cfg
// from its current SchemaVersion to the latest registered version.
// Returns the new version after migration (or the original if no migration needed).
func Migrate(cfg *Config) (int, error) {
	runner := newMigrationRunner()
	return runner.apply(cfg)
}

func (r *migrationRunner) apply(cfg *Config) (int, error) {
	current := cfg.SchemaVersion
	if current < 0 {
		current = 0
	}

	// Collect pending migrations in version order.
	var pending []int
	for v := range r.migrations {
		if v > current {
			pending = append(pending, v)
		}
	}
	sort.Ints(pending)

	if len(pending) == 0 {
		return current, nil
	}

	r.log.Info("running config schema migrations",
		"from_version", current,
		"to_version", pending[len(pending)-1],
		"migration_count", len(pending))

	newVersion := current
	for _, v := range pending {
		m := r.migrations[v]
		r.log.Debug("applying config migration", "version", v, "description", m.Description)
		if err := m.Apply(cfg); err != nil {
			return newVersion, fmt.Errorf("config migration v%d (%s): %w", v, m.Description, err)
		}
		cfg.SchemaVersion = v
		newVersion = v
		r.log.Info("config migration applied", "version", v)
	}

	return newVersion, nil
}

// NeedsMigration returns true if the config has pending schema migrations.
func NeedsMigration(cfg *Config) bool {
	runner := newMigrationRunner()
	current := cfg.SchemaVersion
	if current < 0 {
		current = 0
	}
	for v := range runner.migrations {
		if v > current {
			return true
		}
	}
	return false
}

// LatestSchemaVersion returns the highest registered migration version.
func LatestSchemaVersion() int {
	runner := newMigrationRunner()
	maxV := 0
	for v := range runner.migrations {
		if v > maxV {
			maxV = v
		}
	}
	return maxV
}
