package capability

// Capability set-ID constructors. Set IDs follow the "<kind>:<name>" scheme;
// these helpers are the single place that knows the scheme, so callers never
// hand-roll the concatenation and the format cannot drift between packages.

// MCPSetID returns the set ID for a connected MCP server.
func MCPSetID(serverName string) string { return "mcp:" + serverName }

// ExtensionSetID returns the set ID for a registered process extension.
func ExtensionSetID(name string) string { return "extension:" + name }

// AgentPackSetID returns the set ID for an agent pack loaded from disk.
func AgentPackSetID(packID string) string { return "agent-pack:" + packID }

// SkillSetID returns the set ID for a discovered skill.
func SkillSetID(slug string) string { return "skill:" + slug }
