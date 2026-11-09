# Relay notch UI

The Mac notch island is the only interface. It lists Relay's accounts with
per-account 5-hour and weekly usage, adds accounts through Relay's browser
OAuth, and removes credentials. Relay (the Go proxy in this repo) serves
every account concurrently, so there is no account switching here: nothing
closes, swaps, or rewrites credential files.

## Use

1. Relay runs at `127.0.0.1:8317` (LaunchAgent `com.relay.proxy`).
2. Launch Relay. It appears around the Mac's camera notch, or as a
   floating capsule on a Mac without one.
3. Expand the island to see accounts and usage. Add Account signs in
   through the browser; the account appears when Relay saves it.

Relay talks to Relay's management API with the key in
`~/.relay/management.key` (0600). That value must match
`remote-management.secret-key` in `~/.relay/config.yaml`.

## Build

```bash
swift build
./scripts/package-local-app.sh
```

The app bundle is written to `.build/release/Relay.app`.

## Docs

- [System design](docs/system-design.md)
- [Product decisions](docs/product-decisions.md)

## License

Same mix as before the merge: the app sources are MIT, see
[LICENSE](LICENSE); the notch island implementation copied from the
Vorssaint experiment is GPL-3.0-or-later, see
[LICENSE-GPL-3.0-or-later](LICENSE-GPL-3.0-or-later) and the SPDX headers
in the covered files. The combined Relay app is distributed under
GPL-3.0-or-later. The local app bundle includes both license texts.
