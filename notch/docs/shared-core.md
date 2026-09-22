# Shared core and the macOS client

This supersedes the initial Windows prototype's separate C# account implementation.

## Required design

- Reuse the existing Swift business implementation as a cross-platform `SwitcherCore` package target.
- Both clients share account models, persisted settings, localization, account lifecycle, Codex RPC, weekly/5-hour normalization, the ordered switch sequence, and bounded restoration.
- macOS keeps its SwiftUI menu-bar interface and Sparkle integration.
- User-provided screenshots in the Pictures directory are design references only. Do not copy their real account information into fixtures or repository assets.

## Boundaries

`SwitcherCore` owns policy and state. Platform adapters own filesystem permissions/atomic installation, executable discovery and launch, browser opening, Desktop close/open, launch-at-login, and update installation.

## Parity acceptance

The core tests must run on macOS. They must cover startup registration, explicit register/add/remove, automatic refresh and cache retention, identity checks, direct switch ordering, failure restoration, and cancellation.

`AccountController` and the shared account services drive the macOS interface.
