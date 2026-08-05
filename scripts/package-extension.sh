#!/usr/bin/env bash
# Package an extension plugin into a distributable .tar.gz archive.
# Usage: ./scripts/package-extension.sh <extension-name> [version]
# Example: ./scripts/package-extension.sh channel-discord 1.0.0

set -euo pipefail

NAME="${1:?usage: $0 <extension-name> [version]}"
VERSION="${2:-0.1.0}"
EXT_DIR="extensions/${NAME}"
DIST_DIR="dist/extensions"

if [ ! -d "$EXT_DIR" ]; then
  echo "ERROR: extension directory '$EXT_DIR' not found"
  echo "Available extensions:"
  ls -d extensions/*/ 2>/dev/null | sed 's|extensions/||;s|/||' || echo "  (none)"
  exit 1
fi

if [ ! -f "$EXT_DIR/manifest.json" ]; then
  echo "ERROR: $EXT_DIR/manifest.json not found — every extension needs a manifest"
  exit 1
fi

mkdir -p "$DIST_DIR"

PACKAGE="${NAME}-v${VERSION}"
STAGING="$DIST_DIR/$PACKAGE"
rm -rf "$STAGING"
mkdir -p "$STAGING"

# Copy manifest (with version injected).
if command -v jq &>/dev/null; then
  jq --arg v "$VERSION" '.version = $v' "$EXT_DIR/manifest.json" > "$STAGING/manifest.json"
else
  cp "$EXT_DIR/manifest.json" "$STAGING/manifest.json"
fi

# Build the binary if a cmd/ directory exists.
if [ -d "$EXT_DIR/cmd" ]; then
  echo "Building $NAME..."
  BIN_NAME="${NAME##*-}"  # strip prefix like "channel-"
  go build -o "$STAGING/$BIN_NAME" "./${EXT_DIR}/cmd/"
  chmod +x "$STAGING/$BIN_NAME"
fi

# Copy frontend assets if they exist.
if [ -d "$EXT_DIR/frontend" ]; then
  cp -r "$EXT_DIR/frontend" "$STAGING/"
fi

# Copy README if it exists.
if [ -f "$EXT_DIR/README.md" ]; then
  cp "$EXT_DIR/README.md" "$STAGING/"
fi

# Create tarball.
ARCHIVE="${DIST_DIR}/${PACKAGE}.tar.gz"
tar -czf "$ARCHIVE" -C "$DIST_DIR" "$PACKAGE"
rm -rf "$STAGING"

echo "Packaged: $ARCHIVE ($(du -h "$ARCHIVE" | cut -f1))"
