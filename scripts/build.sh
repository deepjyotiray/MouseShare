#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="$ROOT_DIR/dist"
APP_NAME="MouseShare"
MAC_APP_DIR="$DIST_DIR/${APP_NAME}.app"
MAC_CONTENTS_DIR="$MAC_APP_DIR/Contents"
MACOS_DIR="$MAC_CONTENTS_DIR/MacOS"
RESOURCES_DIR="$MAC_CONTENTS_DIR/Resources"

mkdir -p "$DIST_DIR"

echo "Building macOS app bundle..."
rm -rf "$MAC_APP_DIR"
mkdir -p "$MACOS_DIR" "$RESOURCES_DIR"
GOOS=darwin GOARCH=arm64 go build -o "$MACOS_DIR/$APP_NAME" ./cmd/mouseshare
cp "$ROOT_DIR/packaging/macos/Info.plist" "$MAC_CONTENTS_DIR/Info.plist"
cp "$ROOT_DIR/assets/icons/mouseshare.icns" "$RESOURCES_DIR/mouseshare.icns"
cp "$MACOS_DIR/$APP_NAME" "$DIST_DIR/mouseshare-macos"

echo "Building Windows GUI app..."
GOOS=windows GOARCH=amd64 go build -ldflags="-H windowsgui" -o "$DIST_DIR/MouseShare.exe" ./cmd/mouseshare
cp "$DIST_DIR/MouseShare.exe" "$DIST_DIR/mouseshare-windows.exe"

echo "Artifacts:"
ls -lh "$DIST_DIR"
