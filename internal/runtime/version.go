package runtime

import (
	"fmt"
	"strconv"
	"strings"
)

// ParsedVersion is a parsed semantic version (major.minor.patch).
type ParsedVersion struct {
	Major int
	Minor int
	Patch int
	Raw   string
}

// ParseVersion extracts a semver from common version strings.
// Handles: "v22.11.0", "22.11.0", "node v22.11.0", "Python 3.12.3"
func ParseVersion(raw string) (*ParsedVersion, error) {
	raw = strings.TrimSpace(raw)
	// Strip common prefixes.
	raw = strings.TrimPrefix(raw, "node ")
	raw = strings.TrimPrefix(raw, "Node ")
	raw = strings.TrimPrefix(raw, "python ")
	raw = strings.TrimPrefix(raw, "Python ")
	raw = strings.TrimPrefix(raw, "v")
	raw = strings.TrimPrefix(raw, "V")

	// Split on first space or newline, take the version part.
	if idx := strings.IndexAny(raw, " \n\r"); idx > 0 {
		raw = raw[:idx]
	}

	parts := strings.Split(raw, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("cannot parse version %q", raw)
	}

	v := &ParsedVersion{Raw: raw}
	var err error

	v.Major, err = strconv.Atoi(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid major version in %q: %w", raw, err)
	}
	v.Minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid minor version in %q: %w", raw, err)
	}
	if len(parts) >= 3 {
		// Patch may have suffixes like "3rc1" — extract just the number.
		patchStr := parts[2]
		if idx := strings.IndexFunc(patchStr, func(r rune) bool {
			return (r < '0' || r > '9') && r != '-'
		}); idx > 0 {
			patchStr = patchStr[:idx]
		}
		v.Patch, _ = strconv.Atoi(patchStr)
	}

	return v, nil
}

// Compare returns -1, 0, or 1 if v < other, v == other, v > other.
func (v *ParsedVersion) Compare(other *ParsedVersion) int {
	if v.Major != other.Major {
		return sign(v.Major - other.Major)
	}
	if v.Minor != other.Minor {
		return sign(v.Minor - other.Minor)
	}
	return sign(v.Patch - other.Patch)
}

// AtLeast returns true if v >= min.
func (v *ParsedVersion) AtLeast(min *ParsedVersion) bool {
	return v.Compare(min) >= 0
}

// String returns the canonical version string.
func (v *ParsedVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// NodeMinVersion is the minimum Node.js version required for managed runtime.
var NodeMinVersion = &ParsedVersion{Major: 18, Minor: 0, Patch: 0}

// PythonMinVersion is the minimum Python version required.
var PythonMinVersion = &ParsedVersion{Major: 3, Minor: 8, Patch: 0}

// CheckCompatibility verifies a parsed version meets the minimum.
func CheckCompatibility(kind RuntimeKind, version *ParsedVersion) error {
	switch kind {
	case RuntimeNode:
		if !version.AtLeast(NodeMinVersion) {
			return fmt.Errorf("node.js %s is below minimum %s", version, NodeMinVersion)
		}
	case RuntimePython:
		if !version.AtLeast(PythonMinVersion) {
			return fmt.Errorf("python %s is below minimum %s", version, PythonMinVersion)
		}
	}
	return nil
}

func sign(n int) int {
	if n < 0 {
		return -1
	}
	if n > 0 {
		return 1
	}
	return 0
}
