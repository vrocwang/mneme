// Package store provides SQLite persistence for MCP server installations,
// environment values (stored separately from metadata and never serialized
// in responses), and registry search cache.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/simon/mneme/internal/capability"
)

// InstalledServer is a persisted MCP server record.
type InstalledServer struct {
	ServerID        string   `json:"server_id"`
	QualifiedName   string   `json:"qualified_name"`
	DisplayName     string   `json:"display_name"`
	Description     string   `json:"description,omitempty"`
	IconURL         string   `json:"icon_url,omitempty"`
	Command         string   `json:"command"`
	Args            []string `json:"args"`
	EnvKeys         []string `json:"env_keys"`
	Transport       string   `json:"transport"` // "stdio" or "http_remote"
	DeploymentURL   string   `json:"deployment_url,omitempty"`
	ConfigJSON      string   `json:"config_json,omitempty"`
	Enabled         bool     `json:"enabled"`
	InstalledAt     int64    `json:"installed_at"`
	LastConnectedAt *int64   `json:"last_connected_at,omitempty"`
}

// ToServerEntry converts the persisted record to a capability.ServerEntry
// suitable for ConnectMCPServer.
func (s *InstalledServer) ToServerEntry() capability.ServerEntry {
	return capability.ServerEntry{
		Name:      s.QualifiedName,
		Transport: s.Transport,
		Command:   s.Command,
		Args:      s.Args,
		URL:       s.DeploymentURL,
		Enabled:   s.Enabled,
	}
}

// ConnStatus is a per-server connection status summary.
type ConnStatus struct {
	ServerID      string `json:"server_id"`
	QualifiedName string `json:"qualified_name"`
	DisplayName   string `json:"display_name"`
	Status        string `json:"status"` // connected, disconnected, error, disabled
	ToolCount     int    `json:"tool_count"`
	LastError     string `json:"last_error,omitempty"`
}

// Store provides SQLite-backed persistence for MCP installations.
type Store struct {
	db *sql.DB
	mu sync.Mutex
}

// NewStore initializes the MCP store schema and returns a Store.
func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("mcp store requires a database")
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("mcp store migrate: %w", err)
	}
	return s, nil
}

func (s *Store) migrate() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS mcp_servers (
			server_id         TEXT PRIMARY KEY,
			qualified_name    TEXT NOT NULL,
			display_name      TEXT NOT NULL,
			description       TEXT,
			icon_url           TEXT,
			command           TEXT NOT NULL DEFAULT '',
			args_json         TEXT NOT NULL DEFAULT '[]',
			env_keys_json     TEXT NOT NULL DEFAULT '[]',
			transport         TEXT NOT NULL DEFAULT 'stdio',
			deployment_url    TEXT,
			config_json       TEXT,
			enabled           INTEGER NOT NULL DEFAULT 1,
			installed_at      INTEGER NOT NULL,
			last_connected_at INTEGER
		)`,
		`CREATE TABLE IF NOT EXISTS mcp_client_env (
			server_id TEXT NOT NULL,
			key       TEXT NOT NULL,
			value     TEXT NOT NULL,
			PRIMARY KEY (server_id, key),
			FOREIGN KEY (server_id) REFERENCES mcp_servers(server_id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS mcp_registry_cache (
			cache_key  TEXT PRIMARY KEY,
			body_json  TEXT NOT NULL,
			cached_at  INTEGER NOT NULL
		)`,
	}
	for _, stmt := range statements {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("mcp store migration: %w", err)
		}
	}
	return nil
}

// ── Server CRUD ────────────────────────────────────────────────────────

// InsertServer persists a new MCP server installation.
func (s *Store) InsertServer(server *InstalledServer) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	argsJSON, _ := json.Marshal(server.Args)
	envKeysJSON, _ := json.Marshal(server.EnvKeys)
	if server.InstalledAt == 0 {
		server.InstalledAt = time.Now().UnixMilli()
	}
	_, err := s.db.Exec(
		`INSERT INTO mcp_servers
		 (server_id, qualified_name, display_name, description, icon_url,
		  command, args_json, env_keys_json, transport, deployment_url,
		  config_json, enabled, installed_at, last_connected_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		server.ServerID, server.QualifiedName, server.DisplayName,
		server.Description, server.IconURL, server.Command,
		string(argsJSON), string(envKeysJSON), server.Transport,
		server.DeploymentURL, server.ConfigJSON,
		boolToInt(server.Enabled), server.InstalledAt, server.LastConnectedAt,
	)
	return err
}

// ListServers returns all installed MCP servers, oldest first.
func (s *Store) ListServers() ([]InstalledServer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT server_id, qualified_name, display_name, description, icon_url,
		        command, args_json, env_keys_json, transport, deployment_url,
		        config_json, enabled, installed_at, last_connected_at
		 FROM mcp_servers ORDER BY installed_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanServers(rows)
}

// GetServer returns a single installed server by ID.
func (s *Store) GetServer(serverID string) (*InstalledServer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRow(
		`SELECT server_id, qualified_name, display_name, description, icon_url,
		        command, args_json, env_keys_json, transport, deployment_url,
		        config_json, enabled, installed_at, last_connected_at
		 FROM mcp_servers WHERE server_id = ?`, serverID)
	server, err := scanServer(row)
	if err != nil {
		return nil, fmt.Errorf("mcp server %q not found", serverID)
	}
	return server, nil
}

// DeleteServer removes an installed server and its env values (cascaded).
func (s *Store) DeleteServer(serverID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec("DELETE FROM mcp_servers WHERE server_id = ?", serverID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// UpdateLastConnected sets the last_connected_at timestamp for a server.
func (s *Store) UpdateLastConnected(serverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ts := time.Now().UnixMilli()
	_, err := s.db.Exec("UPDATE mcp_servers SET last_connected_at = ? WHERE server_id = ?", ts, serverID)
	return err
}

// UpdateEnabled flips the enabled flag on an installed server.
func (s *Store) UpdateEnabled(serverID string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("UPDATE mcp_servers SET enabled = ? WHERE server_id = ?", boolToInt(enabled), serverID)
	return err
}

// UpdateEnvKeys updates the env_keys list for a server (after reconfiguring env values).
func (s *Store) UpdateEnvKeys(serverID string, keys []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, _ := json.Marshal(keys)
	_, err := s.db.Exec("UPDATE mcp_servers SET env_keys_json = ? WHERE server_id = ?", string(b), serverID)
	return err
}

// ── Env values ──────────────────────────────────────────────────────────

// SetEnvValues stores (insert or replace) env key-value pairs for a server.
// Values are never returned in list/status responses.
func (s *Store) SetEnvValues(serverID string, env map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Delete all existing env rows, then re-insert the current set.
	if _, err := s.db.Exec("DELETE FROM mcp_client_env WHERE server_id = ?", serverID); err != nil {
		return err
	}
	for k, v := range env {
		if _, err := s.db.Exec(
			"INSERT INTO mcp_client_env (server_id, key, value) VALUES (?, ?, ?)",
			serverID, k, v,
		); err != nil {
			return err
		}
	}
	return nil
}

// LoadEnvValues returns the stored env values for a server. Never serialize
// or log these values.
func (s *Store) LoadEnvValues(serverID string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query("SELECT key, value FROM mcp_client_env WHERE server_id = ?", serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// ── Registry cache ──────────────────────────────────────────────────────

const registryCacheTTL = 10 * time.Minute

// GetCached returns cached registry response body if it exists and is fresh.
func (s *Store) GetCached(cacheKey string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var body string
	var cachedAt int64
	err := s.db.QueryRow(
		"SELECT body_json, cached_at FROM mcp_registry_cache WHERE cache_key = ?",
		cacheKey,
	).Scan(&body, &cachedAt)
	if err != nil {
		return "", false
	}
	if time.Now().UnixMilli()-cachedAt > registryCacheTTL.Milliseconds() {
		return "", false
	}
	return body, true
}

// SetCached stores a registry response body in the cache.
func (s *Store) SetCached(cacheKey, body string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		"INSERT OR REPLACE INTO mcp_registry_cache (cache_key, body_json, cached_at) VALUES (?, ?, ?)",
		cacheKey, body, time.Now().UnixMilli(),
	)
	return err
}

// ── Helpers ──────────────────────────────────────────────────────────────

func scanServers(rows *sql.Rows) ([]InstalledServer, error) {
	var out []InstalledServer
	for rows.Next() {
		s, err := scanServerFromRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func scanServer(row interface{ Scan(...interface{}) error }) (*InstalledServer, error) {
	var s InstalledServer
	var argsJSON, envKeysJSON string
	var lastConn *int64
	var desc, iconURL, deployURL, configJSON *string
	var enabled int64
	if err := row.Scan(
		&s.ServerID, &s.QualifiedName, &s.DisplayName, &desc, &iconURL,
		&s.Command, &argsJSON, &envKeysJSON, &s.Transport, &deployURL,
		&configJSON, &enabled, &s.InstalledAt, &lastConn,
	); err != nil {
		return nil, err
	}
	if desc != nil {
		s.Description = *desc
	}
	if iconURL != nil {
		s.IconURL = *iconURL
	}
	if deployURL != nil {
		s.DeploymentURL = *deployURL
	}
	if configJSON != nil {
		s.ConfigJSON = *configJSON
	}
	s.Enabled = enabled != 0
	s.LastConnectedAt = lastConn
	json.Unmarshal([]byte(argsJSON), &s.Args)
	json.Unmarshal([]byte(envKeysJSON), &s.EnvKeys)
	return &s, nil
}

func scanServerFromRows(rows *sql.Rows) (*InstalledServer, error) {
	var s InstalledServer
	var argsJSON, envKeysJSON string
	var lastConn *int64
	var desc, iconURL, deployURL, configJSON *string
	var enabled int64
	if err := rows.Scan(
		&s.ServerID, &s.QualifiedName, &s.DisplayName, &desc, &iconURL,
		&s.Command, &argsJSON, &envKeysJSON, &s.Transport, &deployURL,
		&configJSON, &enabled, &s.InstalledAt, &lastConn,
	); err != nil {
		return nil, err
	}
	if desc != nil {
		s.Description = *desc
	}
	if iconURL != nil {
		s.IconURL = *iconURL
	}
	if deployURL != nil {
		s.DeploymentURL = *deployURL
	}
	if configJSON != nil {
		s.ConfigJSON = *configJSON
	}
	s.Enabled = enabled != 0
	s.LastConnectedAt = lastConn
	json.Unmarshal([]byte(argsJSON), &s.Args)
	json.Unmarshal([]byte(envKeysJSON), &s.EnvKeys)
	return &s, nil
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// PersistMCPServer adapts capability.PersistedMCPServer to InstalledServer,
// satisfying capability.MCPServerPersister. It is idempotent: any prior row
// for the same server ID is replaced first, so re-adding a server is safe.
func (s *Store) PersistMCPServer(srv *capability.PersistedMCPServer) error {
	_, _ = s.DeleteServer(srv.ServerID) // clear prior row (idempotent upsert)
	return s.InsertServer(&InstalledServer{
		ServerID:      srv.ServerID,
		QualifiedName: srv.QualifiedName,
		DisplayName:   srv.DisplayName,
		Command:       srv.Command,
		Args:          srv.Args,
		Transport:     srv.Transport,
		DeploymentURL: srv.DeploymentURL,
		Enabled:       srv.Enabled,
	})
}

// RemoveMCPServer deletes a persisted server, satisfying capability.MCPServerPersister.
func (s *Store) RemoveMCPServer(serverID string) error {
	_, err := s.DeleteServer(serverID)
	return err
}
