# MouseShare Product Requirements Document

## Date

July 16, 2026

## Vision

MouseShare lets a user move their cursor across the edge of one laptop and continue seamlessly onto another laptop on the same Wi‑Fi, while also sharing keyboard input and allowing file send between trusted devices.

## v1 Scope

- macOS and Windows support
- LAN-only connectivity
- Auto-discover peers on the same Wi‑Fi
- Explicit trust approval
- Manual address + pair-code fallback pairing
- 2D layout configuration
- File send through the app UI
- Unsigned internal testing builds

## User stories

- As a user, I want nearby devices to appear automatically so setup is quick.
- As a user, I want to approve devices before they can control or send files.
- As a user, I want a pair-code fallback when discovery does not work.
- As a user, I want to arrange devices visually so edge switching feels natural.
- As a user, I want files to transfer securely to another trusted device on the LAN.

## Functional requirements

- Devices broadcast presence over the LAN.
- Trusted peer identities persist across restarts.
- Peer traffic uses encrypted transport after pairing.
- Layout configuration persists locally.
- The app surfaces permission/setup status.
- Transfers show progress and final destination.

## Constraints

- No cloud relay in v1.
- No clipboard sync in v1.
- No true cross-edge file drag/drop in v1.
- Native global input capture and injection require platform-specific completion beyond the shared Go core.
