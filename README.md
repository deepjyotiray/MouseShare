# MouseShare

MouseShare is a same-LAN utility for sharing one mouse and keyboard across macOS and Windows laptops, with peer approval, 2D layout configuration, and encrypted file transfer.

## Current status

This repository now includes:

- A Go-based local app with embedded browser UI
- UDP LAN discovery
- TLS device identity and trust persistence
- Auto-discover + approve workflow
- Manual pair by address + pair code
- ZIP-based file transfer over the peer transport
- Persistent layout/config storage
- Cross-target build scripts for macOS and Windows

The OS-specific global input capture/injection bridge is scaffolded behind interfaces and still needs platform-native completion for full cursor/keyboard takeover behavior.

## Run locally

```bash
go run ./cmd/mouseshare
```

The app opens a local browser UI and listens for peers on the same Wi‑Fi.

## Build

```bash
./scripts/build.sh
```

Artifacts are written to `dist/`.

## Manual pairing

1. Launch the app on both machines.
2. If auto-discovery works, approve the peer in the UI.
3. If discovery fails, copy the `IP:41091` address from one machine and enter it on the other with the displayed pair code.

## Permissions

- macOS: Accessibility and related global-input permissions will be required once the native bridge is completed.
- Windows: Firewall access and low-level input permissions may be required once the native bridge is completed.
