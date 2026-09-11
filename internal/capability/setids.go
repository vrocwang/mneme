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

// ChannelsSetID is the set ID under which in-process channel providers (web,
// cli) are registered.
const ChannelsSetID = "builtin:channels"

// UserAgentsSetID and UserAgentsSetName identify the set holding user-defined
// agent overrides (workspace/agents/*.toml) and agents created from the UI.
// They are constants because several packages register into the same set.
const (
	UserAgentsSetID   = "user-agents"
	UserAgentsSetName = "User Agents"
)
