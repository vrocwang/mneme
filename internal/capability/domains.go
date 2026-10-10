package capability

// Domain capability-set IDs for the built-in tool groups that live outside the
// shared "builtin" set. Each ID is used twice by design: as the capability-set
// ID passed to RegisterSetTools, and as the [bundles] disabled gate in
// internal/bundle (whose Bundle* constants alias these). Declaring the value
// here means a domain's identity is written once, so the set name and the
// disable switch cannot drift apart.
//
// Domain packages import capability already, and internal/bundle aliases these
// constants, so neither has to repeat the literal.
const (
	SetMCP     = "mcp"
	SetConfig  = "config"
	SetTodos   = "todos"
	SetSkills  = "skills"
	SetThreads = "threads"
	SetMemory  = "memory"
	SetGoals   = "goals"
	SetDAG     = "dag"
	SetCron    = "cron"
	SetMonitor = "monitor"
)
