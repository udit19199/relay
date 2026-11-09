# Sidekick (personal fork)

A personal macOS notch companion for Codex. The notch island is the only interface. Its expanded screen shows account Usage and account controls together. No menu-bar item, settings page, auto-update, or releases.

## Use

1. Launch Sidekick. It appears around the Mac's camera notch, or as a floating capsule on a Mac without a notch.
2. Expand the island to see saved accounts, their 5-hour and weekly Usage, and account controls.
3. Select an account to switch, or use Add Account to sign in.

Saved accounts stay on this Mac in `~/Library/Application Support/Sidekick/`. On first launch, Sidekick moves existing data from `~/Library/Application Support/Codex Account Switcher/`.

## Behavior

- Compact island: shows weekly usage when the menu-bar clearance check finds room beside the physical notch.
- Expanded island: shows all saved accounts in one list, with compact per-account 5-hour and weekly Usage rows and reset-credit counts when available. The panel grows with the account count within the available screen height; larger lists scroll. Add Account and Quit stay in the footer.
- Usage refresh runs in the background.

## Build

```bash
swift build
swift test
./scripts/package-local-app.sh
```

The app bundle is written to `.build/release/Sidekick.app`.

## Docs

- [Documentation index](docs/README.md)
- [System design](docs/system-design.md)
- [Testing](docs/testing.md)

## License

Original Sidekick code and project artwork are MIT, see [LICENSE](LICENSE). The copied and adapted notch implementation is GPL-3.0-or-later, see [LICENSE-GPL-3.0-or-later](LICENSE-GPL-3.0-or-later) and the source attribution in the affected files.

The combined Sidekick app is distributed under GPL-3.0-or-later. The MIT license remains in force for the original files. Swift source files identify their license with an SPDX header. GPL-covered files also name their upstream source. The local app bundle includes both license texts. You can use and share the app without charging. If you share a built app, provide the corresponding source for that exact build and any other materials the GPL requires.
