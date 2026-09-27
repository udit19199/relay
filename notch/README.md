# Codex Account Switcher (personal fork)

Menu-bar app to switch Codex accounts on this Mac. Personal use only. No auto-update, no releases.

## Use

1. Open the app from the menu bar.
2. Add each account once through browser sign-in.
3. Pick an account, confirm, and let it close, switch, verify, and reopen Codex Desktop.

Saved accounts stay on this Mac in `~/Library/Application Support/Codex Account Switcher/`.

## Settings kept

- Start at login.
- Show percent in menu bar.
- Show 5-hour use.
- Background use refresh.

## Build

```bash
swift build
swift test
./scripts/package-local-app.sh
```

The app bundle is written to `.build/release/Codex Account Switcher.app`.

## Docs

- [Documentation index](docs/README.md)
- [Product requirements](docs/product-requirements.md)
- [System design](docs/system-design.md)
- [Testing](docs/testing.md)

## License

MIT, see [LICENSE](LICENSE).
