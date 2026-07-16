#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="$ROOT_DIR/dist"

mkdir -p "$DIST_DIR"

echo "Building macOS binary..."
GOOS=darwin GOARCH=arm64 go build -o "$DIST_DIR/mouseshare-macos" ./cmd/mouseshare

echo "Building Windows binary..."
GOOS=windows GOARCH=amd64 go build -o "$DIST_DIR/mouseshare-windows.exe" ./cmd/mouseshare

echo "Artifacts:"
ls -lh "$DIST_DIR"
