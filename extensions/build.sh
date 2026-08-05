#!/usr/bin/env bash
# Build all extension binaries.
#
# Cross-platform:  go run cmd/build-extensions/main.go     (any OS)
# Linux/macOS:     bash extensions/build.sh
# Windows:         powershell -File extensions/build.ps1
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_ROOT"

# Prefer the Go-based cross-platform builder when available.
if [ -f "cmd/build-extensions/main.go" ]; then
  echo "=== Building extensions (Go builder) ==="
  exec go run cmd/build-extensions/main.go "$@"
fi

echo "=== Building extensions ==="

EXT_DIR="$PROJECT_ROOT/extensions"
built=0; skipped=0; failed=0

for dir in "$EXT_DIR"/*/; do
  name=$(basename "$dir")
  if [ ! -f "$dir/go.mod" ]; then
    echo "  SKIP $name (no go.mod)"
    skipped=$((skipped + 1))
    continue
  fi

  output="$name"
  if [ -f "$dir/manifest.json" ]; then
    bin=$(python3 -c "import json; print(json.load(open('$dir/manifest.json')).get('binary',''))" 2>/dev/null || true)
    [ -n "$bin" ] && output="$bin"
  fi

  echo "  BUILD $name"
  if (cd "$dir" && go build -ldflags "-s -w" -o "$output" . 2>&1); then
    chmod +x "$dir/$output" 2>/dev/null || true
    echo "    OK: $dir/$output"
    built=$((built + 1))
  else
    echo "    FAIL: $name"
    failed=$((failed + 1))
  fi
done

echo ""
echo "=== Done: $built built, $skipped skipped, $failed failed ==="
