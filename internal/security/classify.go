package security

import (
	"path/filepath"
	"strings"
	"unicode"
)

type CommandClass string

const (
	Read        CommandClass = "read"
	Write       CommandClass = "write"
	Network     CommandClass = "network"
	Install     CommandClass = "install"
	Destructive CommandClass = "destructive"
)

// ── Quoting-aware segment splitting ───────────────────────────────

// splitUnquotedSegments splits a command string by unquoted shell metacharacters
// (;, &&, ||, |, &, newline). Quoted regions (single, double, backslash-escaped)
// are preserved and do not act as splitters. A bare & splits segments (background)
// but file-descriptor redirects (2>&1, &>file) are preserved.
func splitUnquotedSegments(command string) []string {
	var segments []string
	var current strings.Builder
	runes := []rune(command)
	i := 0

	for i < len(runes) {
		c := runes[i]

		// Single-quoted string: consume until matching close quote
		if c == '\'' {
			start := i
			i++
			for i < len(runes) && runes[i] != '\'' {
				i++
			}
			if i < len(runes) {
				i++ // consume closing quote
			}
			current.WriteString(string(runes[start:i]))
			continue
		}

		// Double-quoted string: consume until unescaped close quote
		if c == '"' {
			start := i
			i++
			for i < len(runes) {
				if runes[i] == '\\' && i+1 < len(runes) {
					i += 2
					continue
				}
				if runes[i] == '"' {
					i++
					break
				}
				i++
			}
			current.WriteString(string(runes[start:i]))
			continue
		}

		// Backslash escape
		if c == '\\' && i+1 < len(runes) {
			current.WriteRune(runes[i])
			current.WriteRune(runes[i+1])
			i += 2
			continue
		}

		// Check for segment delimiters at current position
		remaining := string(runes[i:])

		if strings.HasPrefix(remaining, "&&") || strings.HasPrefix(remaining, "||") || strings.HasPrefix(remaining, "|&") {
			if current.Len() > 0 {
				segments = append(segments, strings.TrimSpace(current.String()))
				current.Reset()
			}
			i += 2
			continue
		}

		if c == ';' || c == '|' || c == '\n' {
			if current.Len() > 0 {
				segments = append(segments, strings.TrimSpace(current.String()))
				current.Reset()
			}
			i++
			continue
		}

		// Bare & splits segments (backgrounding) — but skip file-descriptor
		// redirects: >&, <&, and digit+>& (2>&1).
		if c == '&' {
			// Skip if part of && (already handled above).
			// Skip if preceded by > or < (fd redirect).
			// Also skip 2>&1 pattern: digit followed by >&.
			// fd redirect: >&, <&, and digit+>& (e.g. 2>&1) — all handled here
			// since runes[i-1] is '>' or '<' for all three patterns.
			if i > 0 && (runes[i-1] == '>' || runes[i-1] == '<') {
				current.WriteRune(c)
				i++
				continue
			}
			// This is a bare & — split here.
			if current.Len() > 0 {
				segments = append(segments, strings.TrimSpace(current.String()))
				current.Reset()
			}
			i++
			continue
		}

		current.WriteRune(c)
		i++
	}

	if current.Len() > 0 {
		segments = append(segments, strings.TrimSpace(current.String()))
	}

	if len(segments) == 0 {
		return []string{strings.TrimSpace(command)}
	}
	return segments
}

// ── Env prefix detection ──────────────────────────────────────────

var dangerousEnvPrefixes = []string{
	"BASH_ENV", "BAT_PAGER", "BROWSER",
	"CLASSPATH",
	"DYLD_FORCE_FLAT_NAMESPACE",
	"DYLD_INSERT_LIBRARIES", "DYLD_LIBRARY_PATH", "EDITOR", "ENV",
	"GIT_EDITOR", "GIT_EXTERNAL_DIFF", "GIT_EXTERNAL_FILTER",
	"GIT_PAGER", "GIT_SSH", "GIT_SSH_COMMAND",
	"GRADLE_OPTS",
	"IFS",
	"JAVA_TOOL_OPTIONS", "_JAVA_OPTIONS",
	"LD_AUDIT", "LD_LIBRARY_PATH", "LD_ORIGIN_PATH", "LD_PRELOAD", "LESS",
	"LESSCLOSE", "LESSOPEN",
	"MANOPT", "MANPAGER", "MAVEN_OPTS",
	"NODE_OPTIONS", "NODE_PATH",
	"PAGER", "PATH", "PERL5LIB", "PERL5OPT",
	"PROMPT_COMMAND", "PS1", "PS2", "PS3", "PS4",
	"PYTHONPATH", "PYTHONSTARTUP", "PYTHONWARNINGS",
	"RUBYOPT",
	"SHELL", "VISUAL",
}

// hasDangerousEnvPrefix returns true if the command starts with inline env
// assignments that include a dangerous variable name (e.g. GIT_PAGER=evil git log).
func hasDangerousEnvPrefix(s string) bool {
	rest := strings.TrimLeftFunc(s, unicode.IsSpace)
	for {
		word := firstWord(rest)
		if word == "" || !strings.ContainsRune(word, '=') {
			return false
		}
		parts := strings.SplitN(word, "=", 2)
		if len(parts) < 2 {
			return false
		}
		name := strings.ToUpper(parts[0])
		for _, d := range dangerousEnvPrefixes {
			if name == d {
				return true
			}
		}
		rest = strings.TrimSpace(rest[len(word):])
	}
}

// skipEnvAssignments skips leading FOO=bar assignments and returns the remainder.
func skipEnvAssignments(s string) string {
	rest := strings.TrimLeftFunc(s, unicode.IsSpace)
	for {
		word := firstWord(rest)
		if word == "" || !strings.ContainsRune(word, '=') {
			return rest
		}
		rest = strings.TrimSpace(rest[len(word):])
	}
}

// ── Hidden execution detection ────────────────────────────────────

// HasHiddenExecution detects shell structures that can hide a second command from
// classifyCommand: command substitution $(...), variable expansion ${...} (which
// can execute commands via ${cmd} default-value substitution in bash), backticks,
// and process substitution <(...) and >(...). These are blocked outside Full
// autonomy because the inner command bypasses classification entirely.
// Note: bare background & is NOT included here — it is handled as a segment
// delimiter by splitUnquotedSegments, so each side of `cmd1 & cmd2` is
// classified independently.
func HasHiddenExecution(command string) bool {
	// $() and backticks execute commands directly.
	// <(...) and >(...) are process substitution (execute commands).
	// ${} is NOT included — simple variable expansion like ${HOME} does not
	// execute commands. The dangerous forms (${var:-$(cmd)}, ${var:=$(cmd)})
	// are caught by the $() check within the braces.
	if strings.Contains(command, "$(") ||
		strings.Contains(command, "<(") || strings.Contains(command, ">(") {
		return true
	}
	return strings.ContainsRune(command, '`')
}

// containsUnquotedRedirect returns true if the command string contains a `>`
// redirect operator outside of quotes. This prevents "echo 'x > y'" from being
// misclassified as Write. Backtick-quoted regions are also skipped since
// backtick command substitution (`cmd`) is a quoting context where `>` does
// not create a redirect.
func containsUnquotedRedirect(s string) bool {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'':
			for i++; i < len(s) && s[i] != '\''; i++ {
			}
		case '"':
			for i++; i < len(s); i++ {
				if s[i] == '\\' && i+1 < len(s) {
					i++
					continue
				}
				if s[i] == '"' {
					break
				}
			}
		case '`':
			for i++; i < len(s) && s[i] != '`'; i++ {
			}
		case '\\':
			i++
		case '>':
			return true
		}
	}
	return false
}

// ── Command executor detection ────────────────────────────────────

var commandExecutors = []string{
	"env", "eval", "exec",
	"bash", "dash", "fish", "zsh", "ksh", "tcsh", "csh",
	"python", "python3", "python2", "ipython",
	"node", "nodejs", "deno", "bun",
	"ruby", "perl", "php", "lua", "tclsh", "awk", "gawk",
	"pwsh", "powershell",
	"sh", "expect", "groovy", "scala",
	"julia", "r", "Rscript", "octave",
	// Windows LOLBins — living-off-the-land binaries.
	"cmd", "wscript", "cscript", "mshta", "rundll32",
	"iex", "invoke-expression", "start-process",
}

// isCommandExecutor returns true when the base command is a language interpreter
// or shell that can execute arbitrary code.
func isCommandExecutor(base string) bool {
	for _, e := range commandExecutors {
		if base == e {
			return true
		}
	}
	return false
}

// ── Base command extraction ───────────────────────────────────────

// NormalizeBase strips leading env assignments, shell quoting, extracts the
// basename, lowercases it, and strips .exe suffix. Shell quoting (single/double
// quotes around the base command) is stripped to prevent classification bypass
// via e.g. 'sudo' rm -rf /.
func NormalizeBase(cmd string) string {
	cmd = skipEnvAssignments(cmd)
	cmd = strings.TrimSpace(cmd)
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return ""
	}
	base := stripShellQuotes(parts[0])
	base = filepath.Base(base)
	base = strings.TrimSuffix(strings.ToLower(base), ".exe")
	return base
}

// stripShellQuotes removes a single matching pair of single or double quotes
// from the start and end of s.
func stripShellQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// extractArgs returns the lowercased args excluding the base command.
func extractArgs(cmd string) []string {
	cmd = skipEnvAssignments(cmd)
	cmd = strings.TrimSpace(cmd)
	fields := strings.Fields(cmd)
	if len(fields) <= 1 {
		return nil
	}
	args := make([]string, len(fields)-1)
	for i, f := range fields[1:] {
		args[i] = strings.ToLower(f)
	}
	return args
}

// ── Verb-sensitive classification ─────────────────────────────────

var (
	gitReadVerbs = []string{
		"status", "log", "diff", "show", "blame", "branch", "tag",
		"remote", "ls-files", "ls-tree", "rev-parse", "rev-list",
		"describe", "stash", "grep", "config", "shortlog", "whatchanged",
		"cherry", "notes", "reflog", "bisect", "help",
	}
	nodePkgReadVerbs = []string{
		"ls", "list", "view", "info", "outdated", "ping",
		"whoami", "help", "why", "audit", "doctor",
	}
	cargoReadVerbs = []string{
		"tree", "metadata", "search", "info", "version", "help",
	}
	goReadVerbs = []string{
		"version", "env", "doc", "vet", "list", "help",
	}
)

// ── Base command lists ────────────────────────────────────────────

var readOnlyBases = []string{
	"ls", "dir", "cat", "head", "tail", "less", "more", "zless", "zmore",
	"grep", "egrep", "fgrep", "zgrep", "rg", "ag", "ack",
	"find", "which", "whereis", "where", "type", "locate",
	"wc", "sort", "uniq", "cut", "tr", "paste", "join",
	"diff", "cmp", "comm",
	"file", "stat", "du", "df", "free", "uptime", "uname", "hostname",
	"date", "cal", "printenv", "id", "whoami", "who", "w", "last", "groups",
	"pwd", "echo", "printf", "true", "false", "test", "expr",
	"sleep", "wait", "pgrep", "pidof", "ps", "top", "htop",
	"lscpu", "lspci", "lsusb", "lsblk", "lshw", "lsof", "blkid",
	"netstat", "ss", "arp", "host", "dig", "nslookup",
	"ping", "traceroute", "tracepath", "mtr",
	"man", "info", "whatis", "apropos",
	"md5sum", "sha1sum", "sha256sum", "sha512sum", "cksum", "base64", "base32",
	"gunzip", "bunzip2", "unxz", "zipinfo", "unzip",
	"readlink", "realpath", "basename", "dirname",
	"column", "nl", "od", "hexdump", "xxd", "strings",
	"npm", "pnpm", "yarn", "cargo",
	"git",
	"tree", "help",
}

var networkBases = []string{
	"curl", "wget", "nc", "ncat", "netcat", "socat", "telnet",
	"ftp", "sftp", "scp", "rsync", "ssh",
	"http", "https", "aria2c", "axel",
	"nmap", "tcpdump", "tshark",
}

var destructiveBases = []string{
	"shutdown", "reboot", "halt", "poweroff",
	"mkfs", "mke2fs", "mkdosfs", "mkswap",
	"dd", "fdisk", "parted", "sfdisk",
	"iptables", "ip6tables", "nft",
	"chroot", "pivot_root",
	"sudo", "su", "doas", "mount", "umount", "swapoff",
	"useradd", "userdel", "usermod", "passwd", "chpasswd",
	"groupadd", "groupdel",
	"kill", "killall", "pkill", "xkill",
	"chown", "chmod", "chgrp",
	"crontab", "at", "batch",
}

// ── Classification ────────────────────────────────────────────────

// ClassifyCommand classifies a shell command into a CommandClass.
// It splits the command by unquoted shell metacharacters (;, &&, ||, |)
// and classifies each segment independently, taking the max (most dangerous).
// Unknown commands are fail-closed to Write.
func ClassifyCommand(command string) CommandClass {
	// Catastrophic patterns checked before splitting (fork bombs contain | and ;)
	lower := strings.ToLower(command)
	for _, p := range []string{":(){ :|:& };:", ":(){:|:&};:"} {
		if strings.Contains(lower, p) {
			return Destructive
		}
	}

	// Hidden execution (command substitution, backticks, process substitution,
	// background &) bypasses segment classification entirely — the inner command
	// is invisible to classifySegment. Treat as Destructive.
	if HasHiddenExecution(command) {
		return Destructive
	}

	segments := splitUnquotedSegments(command)
	maxClass := Read
	for _, seg := range segments {
		cls := classifySegment(seg)
		if commandClassRank(cls) > commandClassRank(maxClass) {
			maxClass = cls
		}
	}
	return maxClass
}

func classifySegment(segment string) CommandClass {
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return Read
	}

	// Check for dangerous env prefix first (e.g. GIT_PAGER=evil git log)
	if hasDangerousEnvPrefix(segment) {
		return Write
	}

	base := NormalizeBase(segment)
	args := extractArgs(segment)
	joined := strings.ToLower(strings.TrimSpace(skipEnvAssignments(segment)))

	// dd read-only path: when no of= arg is present, dd is purely reading.
	// Must run BEFORE the destructiveBases catch-all so dd invocations with
	// of= still fall through to Destructive (writing to disk devices is
	// destructive; writing to regular files can't be distinguished at
	// classification time).
	if base == "dd" {
		hasOf := false
		for _, a := range args {
			if strings.HasPrefix(a, "of=") {
				hasOf = true
				break
			}
		}
		if !hasOf {
			return Read
		}
	}

	// Destructive commands
	for _, d := range destructiveBases {
		if base == d || strings.HasPrefix(base, d+".") {
			return Destructive
		}
	}

	// mkfs.* variants (mkfs.ext4, mkfs.xfs, etc.)
	if strings.HasPrefix(base, "mkfs") {
		return Destructive
	}

	// BusyBox is a multi-call binary — re-dispatch on the first non-flag
	// argument (the applet name) so that busybox wget → Network,
	// busybox nc → Network, busybox reboot → Destructive, etc.
	if base == "busybox" {
		for _, a := range args {
			if !strings.HasPrefix(a, "-") {
				return classifySegment(a)
			}
		}
		return Write
	}

	// rm targeting root filesystem is destructive (rm -rf /, rm -Rf /, etc.).
	if base == "rm" {
		hasForceRecursive := false
		hasRoot := false
		for _, a := range args {
			al := strings.ToLower(a)
			if al == "-rf" || al == "-fr" || al == "-r" || al == "-f" ||
				strings.HasPrefix(al, "--recursive") || strings.HasPrefix(al, "--force") {
				hasForceRecursive = true
			}
			// Match root paths: "/" exactly, "//" (double-slash root), or
			// top-level absolute dirs like "/etc", "/home".
			if a == "/" || a == "//" || filepath.Clean(a) == "/" ||
				(strings.HasPrefix(a, "/") && !strings.ContainsRune(a[1:], '/')) {
				hasRoot = true
			}
		}
		if hasForceRecursive && hasRoot {
			return Destructive
		}
		// rm without force+recursive on root is Write, not Destructive
		if hasRoot {
			return Write
		}
	}

	// Network commands
	for _, n := range networkBases {
		if base == n {
			return Network
		}
	}

	// Install commands (host-modifying)
	if isInstallCommand(base, args) {
		return Install
	}

	// Interpreters / code executors → Write
	if isCommandExecutor(base) {
		return Write
	}

	// find: read-only unless -exec/-execdir/-ok/-okdir/-delete
	if base == "find" {
		for _, a := range args {
			if a == "-exec" || a == "-execdir" || a == "-ok" || a == "-okdir" || a == "-delete" {
				return Write
			}
		}
		return Read
	}

	// Check for redirect operators (lift to Write) — must come before Read check
	// since "echo hello > file.txt" is Write, not Read.
	// Use quote-aware check so "echo 'x > y'" is not misclassified as Write.
	// Note: pipe-based tee detection is handled by segment splitting (| is a
	// delimiter), so we only need to check for redirects within this segment.
	if containsUnquotedRedirect(joined) {
		return Write
	}

	// Verb-sensitive VCS / package tools
	if base == "git" {
		return verbClass(args, gitReadVerbs)
	}
	if base == "npm" || base == "pnpm" || base == "yarn" {
		return verbClass(args, nodePkgReadVerbs)
	}
	if base == "cargo" {
		return verbClass(args, cargoReadVerbs)
	}
	if base == "go" {
		return verbClass(args, goReadVerbs)
	}

	// Read-only bases
	for _, r := range readOnlyBases {
		if base == r {
			return Read
		}
	}

	// Fail-closed: unknown = Write
	return Write
}

func verbClass(args []string, readVerbs []string) CommandClass {
	if len(args) == 0 {
		return Write
	}
	verb := args[0]
	for _, r := range readVerbs {
		if verb == r {
			return Read
		}
	}
	return Write
}

// ── Install detection ─────────────────────────────────────────────

func isInstallCommand(base string, args []string) bool {
	has := func(needle string) bool {
		for _, a := range args {
			if a == needle {
				return true
			}
		}
		return false
	}
	firstIs := func(verb string) bool {
		if len(args) == 0 {
			return false
		}
		return args[0] == verb
	}

	switch base {
	case "apt", "apt-get", "dnf", "yum", "zypper":
		return has("install")
	case "pacman":
		return isPacmanInstall(args)
	case "apk":
		return has("add")
	case "brew", "snap", "flatpak", "winget", "choco", "scoop":
		return has("install")
	case "pip", "pip3", "pipx", "gem", "go":
		return firstIs("install")
	case "cargo":
		return firstIs("install")
	case "npm", "pnpm":
		return (has("install") || has("i") || has("add")) && (has("-g") || has("--global"))
	case "yarn":
		if !has("global") {
			return false
		}
		// yarn global add/remove/upgrade → Install; yarn global list/bin/dir/info → Read
		for i, a := range args {
			if a == "global" && i+1 < len(args) {
				sub := args[i+1]
				if sub == "list" || sub == "bin" || sub == "dir" || sub == "info" {
					return false
				}
				return true
			}
		}
		return true // global without subcommand = install-like
	}
	return false
}

// isPacmanInstall detects pacman install/upgrade from -S flag without read-only modifiers (s/i/l/g/p).
func isPacmanInstall(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-s") {
			tail := a[2:]
			if !strings.ContainsAny(tail, "silgp") {
				return true
			}
		}
	}
	return false
}

// ── Helpers ───────────────────────────────────────────────────────

func commandClassRank(c CommandClass) int {
	switch c {
	case Read:
		return 0
	case Write:
		return 1
	case Network:
		return 2
	case Install:
		return 3
	case Destructive:
		return 4
	default:
		return 0
	}
}

func firstWord(s string) string {
	s = strings.TrimLeftFunc(s, unicode.IsSpace)
	end := strings.IndexAny(s, " \t")
	if end < 0 {
		return s
	}
	return s[:end]
}
