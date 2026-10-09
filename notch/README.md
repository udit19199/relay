# Sidekick (personal fork)

A personal macOS menu-bar companion for Codex. Switch accounts and check Usage without opening a settings page. No auto-update or releases.

## Use

1. Open the app from the menu bar.
2. Add each account once through browser sign-in.
3. Pick an account, confirm, and let it close, switch, verify, and reopen Codex Desktop.

Saved accounts stay on this Mac in `~/Library/Application Support/Sidekick/`. On first launch, Sidekick moves existing data from `~/Library/Application Support/Codex Account Switcher/`.

## Behavior

- Percent in menu bar: always on.
- 5-hour use line: always on.
- Use refresh runs in the background.

## Build

```bash
swift build
swift test
./scripts/package-local-app.sh
```

The app bundle is written to `.build/release/Sidekick.app`.

## Docs

- [Documentation index](docs/README.md)
- [Product requirements](docs/product-requirements.md)
- [System design](docs/system-design.md)
- [Testing](docs/testing.md)

## License

MIT, see [LICENSE](LICENSE).
