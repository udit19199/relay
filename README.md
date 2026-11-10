# Relay

Relay is a local proxy that gives one API for the AI subscriptions you already
pay for. It serves Claude Code, Codex, Gemini, Antigravity, Kimi, xAI, and
OpenCode Go accounts from a single endpoint on `127.0.0.1:8317`, speaking the
OpenAI, Anthropic, and Gemini protocols. A native macOS notch app shows each
account's limits and manages the credentials.

Relay is a personal project, not a hosted service.

## What it does

- Keeps every signed-in account available at once and rotates across them, so
  one rate-limited account does not stop work.
- Speaks the OpenAI (including Responses), Anthropic, and Gemini wire formats,
  with streaming and tool calls.
- Handles OAuth sign-in for Claude Code, Codex, Kimi, xAI, and other providers,
  and stores credentials as JSON files under `auths/`.
- Serves OpenCode Go natively: Relay reads its rolling, weekly, and monthly
  limits and exposes its models as `opencode-go/<model>`.
- Ships the notch app in `notch/`: per-account 5-hour and weekly usage, Add
  Account, and Remove.

## Quick start

```bash
cp config.example.yaml config.yaml
# put at least one client key under access.api-keys in config.yaml

go build -o relay ./cmd/server
./relay --config config.yaml
```

The server listens on `127.0.0.1:8317`.

```bash
export OPENAI_BASE_URL=http://127.0.0.1:8317/v1
export ANTHROPIC_BASE_URL=http://127.0.0.1:8317
```

Sign in to an account. The sign-in opens your browser:

```bash
./relay --config config.yaml --claude-login
./relay --config config.yaml --codex-login
```

OpenCode Go takes an API key instead of OAuth. Add it from the notch app, or
write `auths/opencode-go.json` with `"type": "opencode-go"` and `"api_key"`.

## Notch app

The island is the only interface for accounts: it lists them with usage, adds
them through Relay's browser sign-in, and removes credentials.

```bash
cd notch
swift build
./scripts/package-local-app.sh   # writes .build/release/Relay.app
```

The app reads the management key from `~/.relay/management.key`. That value must
match `remote-management.secret-key` in `config.yaml`. See
[notch/README.md](notch/README.md) for install and design notes.

## Docs

- [Management API](docs/management-api-v8.md)
- [SDK usage](docs/sdk-usage.md), [access](docs/sdk-access.md),
  [watcher](docs/sdk-watcher.md), [advanced](docs/sdk-advanced.md)
- [LAN discovery](docs/lan-discovery.md)

## Credits

Relay is built on [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) by
Luis Pater and Router-For.ME, used under the MIT license. Relay's git history
includes their work and the work of everyone who contributed to that project.

The protocol translators, provider executors, and management API come from
CLIProxyAPI. Relay adds the macOS notch app and native OpenCode Go support. The
config format, compatibility headers, and routes are unchanged, so existing
clients, plugins, and companion tools keep working.

## License

The proxy is MIT licensed; see [LICENSE](LICENSE). The `notch/` app is
GPL-3.0-or-later as a whole; see
[notch/README.md](notch/README.md#license).
