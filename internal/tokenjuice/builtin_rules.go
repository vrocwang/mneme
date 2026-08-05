package tokenjuice

// loadBuiltinRules returns the built-in compaction rules.
// These mirror the Rust core's vendor rules for the most common tool families.
func loadBuiltinRules() []*JsonRule {
	return []*JsonRule{
		// ── Git family ───────────────────────────────────────────
		{
			ID:         "git/status",
			Family:     "git-status",
			Match:      RuleMatch{Argv0: []string{"git"}, ArgvIncludes: [][]string{{"status"}}},
			Transforms: RuleTransforms{StripANSI: true, TrimEmptyEdges: true, DedupeAdjacent: true},
			SkipPatterns: []string{
				`^On branch `,
				`^Your branch is `,
				`^\(use "git `,
				`^no changes added`,
				`^nothing added to`,
				`^nothing to commit`,
			},
			Summarize: RuleSummarize{Head: 10, Tail: 4},
			Failure:   &RuleFailure{Head: 12, Tail: 12, PreserveOnFail: true},
			Counters: []RuleCounter{
				{Name: "modified", Pattern: `^\s+modified:`},
				{Name: "new file", Pattern: `^\s+new file:`},
				{Name: "deleted", Pattern: `^\s+deleted:`},
				{Name: "renamed", Pattern: `^\s+renamed:`},
			},
		},
		{
			ID:         "git/diff",
			Family:     "git-diff",
			Match:      RuleMatch{Argv0: []string{"git"}, ArgvIncludes: [][]string{{"diff"}}},
			Transforms: RuleTransforms{StripANSI: true},
			Summarize:  RuleSummarize{Head: 20, Tail: 8},
			Failure:    &RuleFailure{Head: 30, Tail: 15, PreserveOnFail: true},
			SkipPatterns: []string{
				`^diff --git `,
				`^index `,
			},
		},
		{
			ID:         "git/log",
			Family:     "git-log",
			Match:      RuleMatch{Argv0: []string{"git"}, ArgvIncludes: [][]string{{"log"}}},
			Transforms: RuleTransforms{StripANSI: true, TrimEmptyEdges: true},
			Summarize:  RuleSummarize{Head: 15, Tail: 0},
		},
		{
			ID:         "git/branch",
			Family:     "git-branch",
			Match:      RuleMatch{Argv0: []string{"git"}, ArgvIncludes: [][]string{{"branch"}}},
			Transforms: RuleTransforms{StripANSI: true, TrimEmptyEdges: true},
			Summarize:  RuleSummarize{Head: 10, Tail: 0},
		},

		// ── Build / compiler output ───────────────────────────────
		{
			ID:         "build/go",
			Family:     "build-go",
			Match:      RuleMatch{Argv0: []string{"go"}, ArgvIncludesAny: [][]string{{"build"}, {"test"}, {"vet"}, {"run"}}},
			Transforms: RuleTransforms{StripANSI: true, TrimEmptyEdges: true},
			Summarize:  RuleSummarize{Head: 0, Tail: 40},
			Failure:    &RuleFailure{Head: 0, Tail: 60, PreserveOnFail: true},
			SkipPatterns: []string{
				`^\? .* \[no test files\]$`,
				`^ok .*\(cached\)$`,
			},
			Counters: []RuleCounter{
				{Name: "errors", Pattern: `\.go:\d+:\d+: `},
			},
		},
		{
			ID:         "build/rust",
			Family:     "build-rust",
			Match:      RuleMatch{Argv0: []string{"cargo"}, ArgvIncludesAny: [][]string{{"build"}, {"test"}, {"check"}, {"clippy"}}},
			Transforms: RuleTransforms{StripANSI: true, TrimEmptyEdges: true},
			Summarize:  RuleSummarize{Head: 0, Tail: 40},
			Failure:    &RuleFailure{Head: 0, Tail: 60, PreserveOnFail: true},
			KeepPatterns: []string{
				`^error`,
				`^warning`,
				`--> `,
				`^\s+\|`,
				`^= `,
				`^note:`,
				`^help:`,
				`^\s+Compiling`,
				`^\s+Finished`,
			},
		},
		{
			ID:         "build/npm",
			Family:     "build-npm",
			Match:      RuleMatch{Argv0: []string{"npm", "npx", "yarn", "pnpm"}},
			Transforms: RuleTransforms{TrimEmptyEdges: true},
			Summarize:  RuleSummarize{Head: 0, Tail: 30},
			Failure:    &RuleFailure{Head: 0, Tail: 50, PreserveOnFail: true},
			KeepPatterns: []string{
				`^npm ERR`,
				`^error `,
				`^Error:`,
				`ERR!`,
				`^WARN`,
				`^warning `,
			},
		},
		{
			ID:         "build/make",
			Family:     "build-make",
			Match:      RuleMatch{Argv0: []string{"make", "cmake", "ninja", "meson"}},
			Transforms: RuleTransforms{TrimEmptyEdges: true},
			Summarize:  RuleSummarize{Head: 0, Tail: 30},
			Failure:    &RuleFailure{Head: 0, Tail: 50, PreserveOnFail: true},
			KeepPatterns: []string{
				`^make(\[\d+\])?:`,
				`^error:`,
				`^Error:`,
				`^fatal `,
			},
		},

		// ── Linter output ─────────────────────────────────────────
		{
			ID:         "lint/eslint",
			Family:     "lint-eslint",
			Match:      RuleMatch{Argv0: []string{"eslint", "npx"}, ArgvIncludesAny: [][]string{{"eslint"}}},
			Transforms: RuleTransforms{TrimEmptyEdges: true},
			Summarize:  RuleSummarize{Head: 0, Tail: 30},
			KeepPatterns: []string{
				`^\s+\d+:\d+\s+`,
				`^\s+✖`,
				`^✖`,
			},
		},

		// ── Package managers ───────────────────────────────────────
		{
			ID:         "pkg/apt",
			Family:     "pkg-apt",
			Match:      RuleMatch{Argv0: []string{"apt", "apt-get", "dpkg"}},
			Transforms: RuleTransforms{TrimEmptyEdges: true},
			Summarize:  RuleSummarize{Head: 0, Tail: 20},
			KeepPatterns: []string{
				`^(Get|Hit|Ign|Err):\d`,
				`^E:`,
				`^W:`,
				`^The following `,
				`^Need to get `,
				`^\d+ upgraded`,
			},
		},

		// ── Network tools ──────────────────────────────────────────
		{
			ID:         "net/curl",
			Family:     "net-curl",
			Match:      RuleMatch{Argv0: []string{"curl", "wget"}},
			Transforms: RuleTransforms{TrimEmptyEdges: true},
			Summarize:  RuleSummarize{Head: 5, Tail: 0},
		},

		// ── System info ────────────────────────────────────────────
		{
			ID:         "sys/ps",
			Family:     "sys-ps",
			Match:      RuleMatch{Argv0: []string{"ps"}},
			Transforms: RuleTransforms{TrimEmptyEdges: true},
			Summarize:  RuleSummarize{Head: 1, Tail: 15},
		},
		{
			ID:         "sys/top",
			Family:     "sys-top",
			Match:      RuleMatch{Argv0: []string{"top", "htop", "btm"}},
			Transforms: RuleTransforms{StripANSI: true, TrimEmptyEdges: true},
			Summarize:  RuleSummarize{Head: 5, Tail: 0},
		},
		{
			ID:         "sys/df",
			Family:     "sys-df",
			Match:      RuleMatch{Argv0: []string{"df", "du"}},
			Transforms: RuleTransforms{TrimEmptyEdges: true},
			Summarize:  RuleSummarize{Head: 1, Tail: 20},
		},
		{
			ID:         "sys/ls",
			Family:     "sys-ls",
			Match:      RuleMatch{Argv0: []string{"ls", "dir"}},
			Transforms: RuleTransforms{TrimEmptyEdges: true},
			Summarize:  RuleSummarize{Head: 0, Tail: 50},
		},
		{
			ID:         "sys/find",
			Family:     "sys-find",
			Match:      RuleMatch{Argv0: []string{"find", "fd", "fdfind"}},
			Transforms: RuleTransforms{DedupeAdjacent: true, TrimEmptyEdges: true},
			Summarize:  RuleSummarize{Head: 0, Tail: 50},
		},

		// ── File content inspection ─────────────────────────────────
		// These rules are intentionally lenient (large head/tail) because
		// file content commands should rarely be compacted.
		{
			ID:         "file/cat",
			Family:     "file-cat",
			Match:      RuleMatch{Argv0: []string{"cat", "bat", "head", "tail", "less", "more"}},
			Transforms: RuleTransforms{StripANSI: true},
			Summarize:  RuleSummarize{Head: 40, Tail: 20},
		},

		// ── Generic fallback (must be last) ─────────────────────────
		{
			ID:         "generic/fallback",
			Family:     "generic",
			Match:      RuleMatch{}, // matches everything
			Transforms: RuleTransforms{StripANSI: true, TrimEmptyEdges: true},
			Summarize:  RuleSummarize{Head: 8, Tail: 8},
		},
	}
}
