# Codex Account Switcher — personal fork

Mac-only build for one user. Keep it trimmed on every upstream pull.

## Keep trimmed

- No auto-update: no Sparkle dependency, no `AppUpdater`, no update UI, no feed or signing scripts.
- No release automation: no `release.yml`, no tag/artifact/feed check scripts.
- No public docs or promo assets: short README, `docs/` stays at index, requirements, design, testing, decisions.
- Tests run with `swift test` only.

## Upstream pulls

When pulling `upstream/main`, keep the deletions above. Resolve conflicts in favor of this fork's trimmed state, then rebuild and run `swift test`.
