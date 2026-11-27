# System design

The notch app is a native macOS island that displays Relay's accounts and
usage. Relay (Go, this repo root) holds every credential under
`~/.relay/auths/`, serves them concurrently with round-robin and
429 failover, and exposes a management API on `127.0.0.1:8317` gated by
a Bearer management key. Relay keeps no credentials and no usage
cache; every number on screen comes from Relay.

## Operations

- List accounts: `GET /v0/management/auth-files` → `files[]` with
  `auth_index`, `name`, `type`/`provider`, `label`, `email`, `disabled`,
  `unavailable`, `status_message`.
- Usage per account: `POST /v0/management/quota/fetch`
  `{"auth_index": ...}` → `groups[].buckets[]` with `window`,
  `remaining_fraction` (0..1), `reset_time`. The UI maps the weekly
  bucket (window mentions week/7d/168h) and the optional 5-hour bucket
  (5h/5-hour/300min) to a `WeeklyUsage` percent + reset date. A missing
  weekly window means "usage unavailable"; a missing 5-hour window
  hides only that row.
- Add account: `GET /v0/management/{anthropic,codex}-auth-url` returns
  `{url, state}`; open the URL in the browser and poll
  `GET /v0/management/get-auth-status?state=` (`wait` → keep polling
  every 2s up to 5 minutes, `ok` → refresh list, `error` → show it).
  Cancel with `DELETE /v0/management/oauth-session?state=`.
- Remove account: `DELETE /v0/management/auth-files` with JSON body
  `{"name": ...}` (the file name from the list).

Auth for all of the above: `Authorization: Bearer <key>`, key read
from `~/.relay/management.key` (0600). Same value as
`remote-management.secret-key` in `~/.relay/config.yaml`.

## UI state

`AccountController` (`@MainActor`) owns `accounts`, `usageStates`
(keyed by account name), `isAddingAccount`, and `visibleError`. A
refresh lists accounts then fetches each quota concurrently; failures
keep the last good value with a warning, or show unavailable. The
compact island shows the lowest weekly remaining across accounts.

## What is intentionally absent

Switching, credential files, Desktop control, token refresh, banked
resets, retry frameworks, persistence, telemetry. Relay does serving,
routing, and refreshing; the island only shows and asks.
