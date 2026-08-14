package security

import (
	"fmt"
	"strings"
	"sync"
)

type Tier string

const (
	TierReadOnly   Tier = "readonly"
	TierSupervised Tier = "supervised"
	TierFull       Tier = "full"
)

type Decision string

const (
	Allow  Decision = "allow"
	Prompt Decision = "prompt"
	Block  Decision = "block"
)

func GateDecision(class CommandClass, tier Tier) Decision {
	switch tier {
	case TierReadOnly:
		if class == Read {
			return Allow
		}
		return Block
	case TierSupervised:
		if class == Read {
			return Allow
		}
		return Prompt
	case TierFull:
		if class == Read || class == Write {
			return Allow
		}
		return Prompt
	default:
		return Block
	}
}

// RiskLevel classifies how dangerous a command is within its CommandClass.
// This is independent of the autonomy tier — even in Full tier, high-risk
// commands can be blocked outright, and medium-risk commands can require
// additional approval in Supervised mode.
type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

// GateDecisionWithRisk extends GateDecision with risk-level awareness.
// When blockHighRisk is true, RiskHigh commands are always blocked regardless of tier.
// When requireMediumApproval is true, RiskMedium commands require approval in Supervised mode.
func GateDecisionWithRisk(class CommandClass, tier Tier, risk RiskLevel, blockHighRisk, requireMediumApproval bool) Decision {
	// High-risk commands can be blocked entirely, even in Full tier.
	if blockHighRisk && risk == RiskHigh {
		return Block
	}
	// Medium-risk commands can require approval in Supervised mode.
	if requireMediumApproval && risk == RiskMedium && tier == TierSupervised && class != Read {
		return Prompt
	}
	return GateDecision(class, tier)
}

// ClassifyRisk returns the risk level for a base command.
func ClassifyRisk(base string) RiskLevel {
	for _, r := range highRiskBases {
		if base == r {
			return RiskHigh
		}
	}
	for _, r := range mediumRiskBases {
		if base == r {
			return RiskMedium
		}
	}
	return RiskLow
}

var highRiskBases = []string{
	"shutdown", "reboot", "halt", "poweroff",
	"mkfs", "mke2fs", "mkdosfs", "mkswap",
	"dd", "fdisk", "parted", "sfdisk",
	"iptables", "ip6tables", "nft",
	"chroot", "pivot_root",
	"sudo", "su", "doas",
	"useradd", "userdel", "usermod", "passwd", "chpasswd",
	"groupadd", "groupdel",
	"kill", "killall", "pkill", "xkill",
	"chown", "chmod", "chgrp",
	"crontab",
}

var mediumRiskBases = []string{
	"curl", "wget", "nc", "ncat", "netcat",
	"scp", "rsync", "ssh",
	"mount", "umount",
	"pip", "pip3", "gem", "npm", "pnpm", "yarn",
	"apt", "apt-get", "dnf", "yum", "zypper", "pacman", "brew",
	"git",
	"docker", "podman",
}

// CheckGatedCommand performs defense-in-depth validation on a shell command.
// It classifies the command, applies the gate decision for the current tier,
// and blocks hidden execution constructs ($(), backticks, <(), background &),
// background chaining, and dangerous env-var overrides in non-Full tiers.
func CheckGatedCommand(command string, tier Tier, blockHighRisk, requireMediumApproval bool) (CommandClass, Decision, error) {
	class := ClassifyCommand(command)
	risk := ClassifyRisk(NormalizeBase(command))
	decision := GateDecisionWithRisk(class, tier, risk, blockHighRisk, requireMediumApproval)
	if decision == Block {
		return class, decision, fmt.Errorf(
			"security policy: this tier does not permit commands classified as %s; use a read-only approach or report that it cannot be done in this mode",
			string(class),
		)
	}
	// In readonly tier, block hidden execution that smuggles unseen commands.
	// supervised and full tiers allow command substitution as it's a core shell feature.
	if tier == TierReadOnly && HasHiddenExecution(command) {
		return class, decision, fmt.Errorf(
			"command/process substitution ($(…), <(…)), backticks are not allowed in this mode; rewrite the command without these constructs",
		)
	}
	// Background execution chaining (cmd1 & cmd2) runs commands unseen by classification.
	// Only blocked in readonly tier; supervised and full allow it as a core shell feature.
	if tier == TierReadOnly && hasBackgroundChain(command) {
		return class, decision, fmt.Errorf(
			"security policy: background execution chaining via & is not allowed in this mode; run commands sequentially instead",
		)
	}
	// In readonly tier, block dangerous env-var overrides that can hijack tool behavior.
	if tier == TierReadOnly && hasDangerousEnvPrefix(command) {
		return class, decision, fmt.Errorf(
			"security policy: overriding environment variables that affect tool behavior (e.g. LD_PRELOAD, GIT_PAGER, NODE_OPTIONS) is not allowed in this mode; remove the env assignment and run the command directly",
		)
	}
	return class, decision, nil
}

// hasBackgroundChain detects bare `&` used to chain commands (not && or &> or &>>).
// e.g. "sleep 10 & rm -rf /" would classify only "sleep 10" and miss "rm -rf /".
func hasBackgroundChain(command string) bool {
	for i := 0; i < len(command); i++ {
		if command[i] == '&' {
			// Skip &&, &>, &>>
			if i+1 < len(command) {
				next := command[i+1]
				if next == '&' || next == '>' {
					i++
					continue
				}
			}
			// Check preceding char isn't > (for >& redirect merge)
			if i > 0 && command[i-1] == '>' {
				continue
			}
			// Skip trailing & at end of string
			if i+1 >= len(command) {
				continue
			}
			// Check if there's a non-whitespace command after &
			rest := strings.TrimLeftFunc(command[i+1:], isSpace)
			if rest != "" && !strings.HasPrefix(rest, "#") {
				return true
			}
		}
	}
	return false
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t'
}

// extraAllowedCommands holds user-configured additional commands allowed in supervised tier.
var (
	extraAllowedCommands []string
	extraAllowedMu       sync.RWMutex
)

// SetExtraAllowedCommands updates the user-configured extra allowed commands list.
// These commands are merged with the hardcoded supervisedCommandAllowlist.
func SetExtraAllowedCommands(cmds []string) {
	extraAllowedMu.Lock()
	defer extraAllowedMu.Unlock()
	extraAllowedCommands = cmds
}

// commandPolicy holds the two gate toggles previously hardcoded by the shell
// tool: whether high-risk commands are always blocked and whether medium-risk
// commands require approval in supervised mode.
var (
	commandPolicy   = struct{ blockHighRisk, requireMediumApproval bool }{blockHighRisk: true, requireMediumApproval: true}
	commandPolicyMu sync.RWMutex
)

// SetCommandPolicy updates the high/medium risk gate toggles from config.
func SetCommandPolicy(blockHighRisk, requireMediumApproval bool) {
	commandPolicyMu.Lock()
	defer commandPolicyMu.Unlock()
	commandPolicy.blockHighRisk = blockHighRisk
	commandPolicy.requireMediumApproval = requireMediumApproval
}

// CommandPolicy returns the current high/medium risk gate toggles.
func CommandPolicy() (blockHighRisk, requireMediumApproval bool) {
	commandPolicyMu.RLock()
	defer commandPolicyMu.RUnlock()
	return commandPolicy.blockHighRisk, commandPolicy.requireMediumApproval
}

// IsCommandAllowed checks whether a command base is in the supervised allowlist.
// Only enforced in TierSupervised; Full tier bypasses the allowlist entirely.
// Unknown commands are blocked (fail-closed) in supervised mode.
// User-configured ExtraAllowedCommands are merged with the hardcoded list.
func IsCommandAllowed(command string) bool {
	base := NormalizeBase(command)
	if base == "" {
		return false
	}
	for _, allowed := range supervisedCommandAllowlist {
		if base == allowed {
			return true
		}
	}
	extraAllowedMu.RLock()
	defer extraAllowedMu.RUnlock()
	for _, allowed := range extraAllowedCommands {
		if base == allowed {
			return true
		}
	}
	return false
}

// supervisedCommandAllowlist is the curated set of base commands permitted in
// Supervised autonomy tier. This mirrors the Rust is_command_allowed allowlist.
// Commands not in this list are blocked in Supervised mode regardless of their
// CommandClass — the LLM can suggest a command but it won't execute.
var supervisedCommandAllowlist = []string{
	// Shell keywords and builtins (part of shell syntax, not external commands)
	"if", "for", "while", "case", "function", "declare", "local",
	"return", "exit", "export", "source", "eval", "exec", "trap",
	"set", "unset", "shift", "read", "umask", "alias", "unalias",
	"jobs", "fg", "bg", "kill", "hash", "help", "history",
	"let", "builtin", "command", "type", "getopts",
	"pushd", "popd", "shopt", "suspend",
	// File operations
	"ls", "dir", "cat", "head", "tail", "less", "more",
	"grep", "egrep", "fgrep", "rg", "find", "locate",
	"wc", "sort", "uniq", "cut", "tr",
	"diff", "cmp",
	"file", "stat", "du", "df",
	"pwd", "echo", "printf", "true", "false", "test",
	"sleep", "wait",
	// Process inspection
	"ps", "pgrep", "pidof", "top",
	// System info
	"date", "uname", "hostname", "uptime", "free",
	"lscpu", "lsblk", "lspci", "lsusb",
	// Network inspection
	"ping", "host", "dig", "nslookup", "ip",
	// Hashing / checksums
	"md5sum", "sha1sum", "sha256sum", "sha512sum",
	// Archive read
	"tar", "gzip", "gunzip", "bzip2", "bunzip2", "xz", "unzip",
	// VCS (read-only verbs only)
	"git",
	// Package managers (read-only verbs only, gate blocks install subcommands)
	"npm", "pnpm", "yarn", "npx", "cargo",
	// Internal
	"which", "type", "whereis",
}
