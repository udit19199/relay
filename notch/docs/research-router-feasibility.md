# No-restart account router for Codex — feasibility research

Scope: can a Mac menu-bar app auto-switch Codex accounts when limits run out **without restart** —
i.e. running tasks keep going but new API requests use another account?

Upstream revision investigated: `openai/codex` @ `abc8f0c9a1ba5754f4bf71343f50283a7a72dcff` (2026-09-27).
All `codex-rs/...` links below are pinned to that revision. Anything not found in primary
sources is marked **NOT FOUND**. Codex Desktop itself is closed-source and **NOT FOUND** in
`openai/codex`; §5 states exactly what primary sources do and do not show.

## 1. Auth loading: `CODEX_HOME/auth.json` is read once, cached, never watched

- Auth file path is `<codex_home>/auth.json`.
  Source: `codex-rs/login/src/auth/storage.rs:154-156` (`get_auth_file` joins `"auth.json"`).
  https://github.com/openai/codex/blob/abc8f0c9a1ba5754f4bf71343f50283a7a72dcff/codex-rs/login/src/auth/storage.rs#L154-L156
- `CODEX_HOME` env var is resolved **once at startup** (must exist and be a directory, else fatal;
  default `~/.codex`).
  Source: `codex-rs/utils/home-dir/src/lib.rs:13-18` (`find_codex_home`).
  https://github.com/openai/codex/blob/abc8f0c9a1ba5754f4bf71343f50283a7a72dcff/codex-rs/utils/home-dir/src/lib.rs#L13-L18
- `AuthManager` is documented as load-once: it "loads once (or on preference change) and then hands
  out cloned `CodexAuth` values", and **"External modifications to `auth.json` will NOT be observed
  until `reload()` is called explicitly."**
  Source: `codex-rs/login/src/auth/manager.rs:2034-2041`.
  https://github.com/openai/codex/blob/abc8f0c9a1ba5754f4bf71343f50283a7a72dcff/codex-rs/login/src/auth/manager.rs#L2034-L2041
- Explicit reload exists as `AuthManager::reload()` ("Reloads auth from the active source. Returns
  whether the auth value changed").
  Source: `codex-rs/login/src/auth/manager.rs:2480-2485`.
  https://github.com/openai/codex/blob/abc8f0c9a1ba5754f4bf71343f50283a7a72dcff/codex-rs/login/src/auth/manager.rs#L2480-L2485
- **No signal, file watcher, or auto-reload for `auth.json` was found.** Grep over `codex-rs` for
  `SIGHUP`/`SIGUSR`/auth file-watching shows only sandbox/PTY signal handling and MCP/user-verification
  watchers — nothing that re-reads `auth.json`. **Signal-to-reload: NOT FOUND.**
- The automatic 401 recovery path **refuses account switches by design**: `reload_if_account_id_matches`
  skips the reload when the on-disk account id differs from the running account ("Skipping auth reload
  due to account id mismatch").
  Source: `codex-rs/login/src/auth/manager.rs:2487-2520`, and the `UnauthorizedRecovery` state-machine
  comment (`manager.rs:1849-1857`): managed ChatGPT recovery = (1) guarded same-account reload from
  disk, (2) OAuth token refresh; otherwise the error bubbles to the user.
  https://github.com/openai/codex/blob/abc8f0c9a1ba5754f4bf71343f50283a7a72dcff/codex-rs/login/src/auth/manager.rs#L2487-L2520
  https://github.com/openai/codex/blob/abc8f0c9a1ba5754f4bf71343f50283a7a72dcff/codex-rs/login/src/auth/manager.rs#L1849-L1857
- Per-request auth reads the **current** manager snapshot (`ConfiguredModelProvider::auth()` calls
  `auth_manager.auth().await` fresh on every request; `api_auth_for_scope` re-resolves per request).
  So a `reload()` *is* visible to the **next** request — but see §3/§4 for why the in-flight turn still
  dies.
  Source: `codex-rs/model-provider/src/provider.rs:467-474` (`auth`), `:492-511`
  (`api_auth_for_scope`), and first-party path predicate `:360-366`.
  https://github.com/openai/codex/blob/abc8f0c9a1ba5754f4bf71343f50283a7a72dcff/codex-rs/model-provider/src/provider.rs#L467-L474
- Separately, there is an identity-pinned provider (`auth_provider_from_auth_manager`) that keeps
  serving the **startup** snapshot and returns **empty headers after an account switch** ("a later
  account switch cannot reuse it" / `is_expected_auth` compares account id + user id + workspace flag).
  It is used for MCP/apps/analytics paths, not the main sampling loop — i.e. those surfaces fail
  closed on switch rather than migrating.
  Source: `codex-rs/model-provider/src/auth.rs:126-169` (provider) and `:327-340` (doc comment);
  callers in `codex-rs/codex-mcp/src/connection_manager.rs:306`,
  `codex-rs/cli/src/exec_server_command.rs:395`,
  `codex-rs/backend-client/src/analytics_session.rs:55`.
  https://github.com/openai/codex/blob/abc8f0c9a1ba5754f4bf71343f50283a7a72dcff/codex-rs/model-provider/src/auth.rs#L126-L169

**§1 bottom line:** swapping `~/.codex/auth.json` under a running process does nothing until
something calls `reload()` (or a login RPC, §4). There is no signal or watcher. The built-in 401
auto-recovery explicitly ignores different-account files.

## 2. Custom endpoint / proxy: supported, but it is a config-layer base-URL override, not an env var

- `openai_base_url` is a user-level `config.toml` key: "Base URL override for the built-in `openai`
  model provider."
  Source: `codex-rs/config/src/config_toml.rs:424`, applied in
  `codex-rs/core/src/config/mod.rs:3799-3805`
  (`built_in_model_providers(openai_base_url)` → `merge_configured_model_providers`).
  https://github.com/openai/codex/blob/abc8f0c9a1ba5754f4bf71343f50283a7a72dcff/codex-rs/config/src/config_toml.rs#L424
  https://github.com/openai/codex/blob/abc8f0c9a1ba5754f4bf71343f50283a7a72dcff/codex-rs/core/src/config/mod.rs#L3799-L3805
  Docs: https://learn.chatgpt.com/docs/config-file/config-advanced ("If you just need to point the
  built-in OpenAI provider at an LLM proxy, router … set `openai_base_url`"),
  https://learn.chatgpt.com/docs/config-file/config-reference (`openai_base_url` row).
- Built-in provider construction: `create_openai_provider(base_url)` (`model-provider-info/src/lib.rs:518-557`),
  assembled by `built_in_model_providers(openai_base_url)` (`:650-654`).
  https://github.com/openai/codex/blob/abc8f0c9a1ba5754f4bf71343f50283a7a72dcff/codex-rs/model-provider-info/src/lib.rs#L518-L557
- A `[model_providers.openai]` table does **not** override the built-in provider: custom entries are
  merged with `entry(key).or_insert(provider)` (existing built-ins win, except the Bedrock-specific
  override path), and `openai`/`ollama`/`lmstudio` are reserved ids.
  Source: `model-provider-info/src/lib.rs:684-724` (merge, see `:719`).
  Docs confirm: "To change the base URL for the built-in OpenAI provider, use `openai_base_url`;
  don't create `[model_providers.openai]`."
  https://github.com/openai/codex/blob/abc8f0c9a1ba5754f4bf71343f50283a7a72dcff/codex-rs/model-provider-info/src/lib.rs#L684-L724
- Effective base URL (`to_api_provider`, `:419-437`): caller `base_url` wins when set; otherwise
  ChatGPT-family auth modes default to `CHATGPT_CODEX_BASE_URL =
  "https://chatgpt.com/backend-api/codex"` (`:77`), everything else to
  `"https://api.openai.com/v1"`. ChatGPT bearer auth is attached independently of `base_url`
  (`model-provider/src/auth.rs:307-325` `auth_provider_from_auth`), so a proxy set via
  `openai_base_url` **does receive the ChatGPT bearer token** — proxy-side failover would have to
  implement account switching itself.
  https://github.com/openai/codex/blob/abc8f0c9a1ba5754f4bf71343f50283a7a72dcff/codex-rs/model-provider-info/src/lib.rs#L418-L467
- Custom providers with `requires_openai_auth = true` reuse OpenAI (ChatGPT/API-key) auth and ignore
  `env_key` — the documented hook for "access OpenAI models through an LLM proxy server".
  Docs: https://learn.chatgpt.com/docs/auth ("Alternative model providers" → OpenAI authentication).
  Code: `model-provider/src/auth.rs:197-222` (`resolve_provider_auth`),
  `model-provider/src/provider.rs:360-366` (`provider_uses_first_party_auth_path`).
  Note `wire_api` only supports `"responses"` (`model-provider-info/src/lib.rs:101-130`).
- **No generic `OPENAI_BASE_URL` env var for model traffic.** The only `OPENAI_BASE_URL` references in
  non-test Rust code are the network-proxy **credential broker** scope
  (`codex-rs/network-proxy/src/credential_broker/providers/openai.rs:14`) and its config plumbing
  (`core/src/config/mod.rs:3716`, `network-proxy/src/config.rs:221`). Model routing is config-driven.
  A proxy therefore requires writing `~/.codex/config.toml` (or `-c` CLI overrides / profile layers),
  which are read at startup/config load — not per-request.
- `openai_base_url` (and `model_providers`, `model_provider`, `chatgpt_base_url`) are **user-level
  only**: Codex ignores them in project-local `.codex/config.toml` with a startup warning.
  Source: `codex-rs/config/src/loader/mod.rs:89` (ignore list) + docs
  (config-advanced "Project config files", config-reference preamble).
  https://github.com/openai/codex/blob/abc8f0c9a1ba5754f4bf71343f50283a7a72dcff/codex-rs/config/src/loader/mod.rs#L89

**§2 bottom line:** pointing model traffic at a local proxy is supported (`openai_base_url` or a
custom provider with `requires_openai_auth = true`), and the proxy would see the normal bearer
credential. But upstream provides no per-request account-switch hook: the proxy itself would have to
detect `usage_limit_reached` and re-issue the request under a different upstream credential. That is
viable for API-key providers; for ChatGPT OAuth it inherits the single-use-refresh-token brittleness
noted in §4.

## 3. Rate-limit signal: `429 + usage_limit_reached` → terminal `UsageLimitReached`; no failover hook

- HTTP mapping (`codex-rs/codex-api/src/api_bridge.rs:179-238`): on `429 TOO_MANY_REQUESTS` the body
  is parsed; `error.type == "usage_limit_reached"` returns
  `CodexErr::UsageLimitReached(UsageLimitReachedError { plan_type, resets_at, limit_window_minutes,
  rate_limits, promo_message, rate_limit_reached_type })` (`:186-216`, limit id from
  `x-codex-active-limit` header, reached-type from headers). `usage_not_included` → `UsageNotIncluded`;
  quota codes → `QuotaExceeded`; any other 429 → `RetryLimit { status, request_id }` (`:235-238`).
  https://github.com/openai/codex/blob/abc8f0c9a1ba5754f4bf71343f50283a7a72dcff/codex-rs/codex-api/src/api_bridge.rs#L179-L238
- Streaming mapping: `response.failed` events with `code "rate_limit_exceeded"`/`"slow_down"` become
  `ApiError::RateLimitExceeded { message, retry_after }` (retry delay parsed from "try again in N
  s/ms/seconds" text); websocket `usage_limit_reached` likewise maps to `UsageLimitReached`.
  Source: `codex-rs/codex-api/src/sse/responses_error.rs:82-88` (+`:96-134` delay regex),
  `codex-rs/codex-api/src/endpoint/responses_websocket.rs` (~`:1016-1085`-wrapping tests).
  https://github.com/openai/codex/blob/abc8f0c9a1ba5754f4bf71343f50283a7a72dcff/codex-rs/codex-api/src/sse/responses_error.rs#L82-L88
- Error shape: `UsageLimitReachedError { plan_type, resets_at, limit_window_minutes,
  rate_limits: Option<Box<RateLimitSnapshot>>, promo_message, rate_limit_reached_type }`
  (`codex-rs/protocol/src/error.rs`, `UsageLimitReachedError` struct ~`:676-683`, Display ~`:686+`);
  reached-type variants include `RateLimitReached`, `WorkspaceOwner/MemberCreditsDepleted`,
  `WorkspaceOwner/MemberUsageLimitReached` (`codex-rs/protocol/src/protocol.rs:2372-2385`).
  `RateLimitSnapshot` (primary/secondary windows, percent remaining) also arrives out-of-band via
  `ResponseEvent::RateLimits` → `sess.update_rate_limits` (`core/src/session/turn.rs:2898-2901`,
  `core/src/session/mod.rs:4886+`).
- **Terminal vs retryable** (`protocol/src/error.rs:384-407`, `CodexErr::retry_delay`): `UsageLimitReached`
  (with `ServerOverloaded`, `FlexUnavailable`, quota/policy errors) returns `None` = terminal;
  `RateLimitExceeded`/`Stream`/`Timeout`/`ConnectionFailed`/etc. get backoff delays = retryable.
  https://github.com/openai/codex/blob/abc8f0c9a1ba5754f4bf71343f50283a7a72dcff/codex-rs/protocol/src/error.rs#L384-L407
- The turn loop special-cases the terminal error **before** the retry handler: on
  `CodexErrorDetails::UsageLimitReached(e)` it records rate limits and `return Err(err)` immediately
  (`core/src/session/turn.rs:1679-1685`); only other errors reach `handle_response_stream_error`.
  https://github.com/openai/codex/blob/abc8f0c9a1ba5754f4bf71343f50283a7a72dcff/codex-rs/core/src/session/turn.rs#L1679-L1685
- The shared retry helper (`core/src/responses_retry.rs:52-151`) implements same-provider backoff and
  a WebSocket→HTTPS **transport** fallback (`:99-114`) — **no alternate-account / failover branch.
  Per-request failover hook: NOT FOUND.**
- `ModelClientSession` is turn-scoped and caches WebSocket + sticky routing state, reused across
  retries within the turn (`turn.rs` comment ~`:415-416`).

**§3 bottom line:** the limit-exhausted signal is precise and machine-readable (429 + typed body +
headers + `RateLimitSnapshot`), but upstream treats it as terminal for the turn. There is no hook to
retry the same turn under a different account; the turn errors out and only the *next* user turn could
use rotated credentials.

## 4. App-server: one process-wide credential; two homes need two processes

- `MessageProcessor` owns a single `auth_manager: Arc<AuthManager>` (`app-server/src/message_processor.rs:259`)
  and clones it into the single process-wide `ThreadManager`
  (`:326-376` — `ThreadManager::new(…, auth_manager.clone(), build_models_manager(…, auth_manager.clone()), …)`).
  The thread store is "intentionally process-scoped" (`:299-302`).
  https://github.com/openai/codex/blob/abc8f0c9a1ba5754f4bf71343f50283a7a72dcff/codex-rs/app-server/src/message_processor.rs#L259-L376
- Thread spawn carries that one manager (`core/src/thread_manager.rs:318-331` `ThreadSpawnRequest {
  auth_manager, … }`); the only per-thread auth pinning is `InternalSessionParent`, which binds a
  child to its **parent's** manager — still one account per process, not one per session.
  (`thread_manager.rs:243-253`.)
- A no-restart credential swap **within one process** exists but is process-wide and login-shaped:
  `account/*` RPCs — API-key login writes `config.codex_home` then `auth_manager.reload()`
  (`app-server/src/request_processors/account_processor.rs:448-458`); external ChatGPT tokens go
  through `set_external_auth` (`:830-873`, bridging to `AuthManager::set_external_auth`,
  `login/src/auth/manager.rs:2663-2689`). After such a swap, already-running turns do **not**
  migrate: their turn already failed terminally (§3), and pinned consumers fail closed (§1).
  https://github.com/openai/codex/blob/abc8f0c9a1ba5754f4bf71343f50283a7a72dcff/codex-rs/app-server/src/request_processors/account_processor.rs#L448-L458
- **Two sessions with different `CODEX_HOME` concurrently = two `codex app-server --stdio`
  processes.** `Config.codex_home` is fixed per process; the daemon resolves it via `find_codex_home()`
  and derives the control socket from it (`app-server-daemon/src/lib.rs:322-323`), and backend spawn
  preserves `CODEX_HOME` in the child env (`backend/pid_start.rs:148-170`, `CODEX_HOME` in the
  preserved list). **No single-instance lock on the `--stdio` path was found (NOT FOUND)** — the
  per-home control socket/state layout implies distinct homes run side by side.
- Caution for dual-home concurrency (issue report, not code): OAuth refresh tokens are single-use, so
  two homes sharing copied `auth.json` content invalidate each other on refresh. Open feature request
  for shared-auth/isolated-config: https://github.com/openai/codex/issues/15410. Each concurrent
  process therefore needs its **own** login's `auth.json`, not a copy of one file.

**§4 bottom line:** `codex app-server --stdio` holds one credential per process, shared by all its
threads. Concurrent accounts require concurrent processes with distinct `CODEX_HOME`s. In-process
login RPCs can swap the process-wide account without restart, but only between turns — never
mid-turn.

## 5. Desktop: closed-source; what primary sources actually show

- **Codex Desktop source: NOT FOUND in `openai/codex`.** The repo has no desktop app directory; the
  only "Desktop" references in primary sources are (a) `codex-rs/app-server/README.md` describing
  "local stdio desktop sessions (`Codex Desktop`)" for the user-verification/MCP capability, and (b)
  user-level `desktop.*` config keys (custom file handlers) in the docs
  (https://learn.chatgpt.com/docs/config-file/config-advanced#desktop).
- What the open-source backend proves: any backend (whether spawned by Desktop, the daemon, or the
  CLI) resolves `CODEX_HOME` via `find_codex_home()`, stores `codex_home` in `Config`, reads
  `<codex_home>/auth.json` through `AuthManager`, and serves model traffic from that identity
  (§1+§4: `app-server-daemon/src/lib.rs:322-323`, `app-server/src/in_process.rs:429`,
  `app-server/src/config_manager.rs:45,99`). Spawns preserve `CODEX_HOME`
  (`app-server-daemon/src/backend/pid_start.rs:148-170`).
- Consequence (inference, marked as such): a Desktop-spawned backend bound to `~/.codex` reads that
  home directly and sends model traffic to the ChatGPT backend (or `openai_base_url` if configured).
  **An external local proxy is bypassed unless the effective user-level config points model traffic
  at it** — there is no documented Desktop setting that redirects model traffic, and project-level
  provider keys are ignored (§2). Whether the Desktop app respects `~/.codex/config.toml`'s
  `openai_base_url` for its embedded backend is **NOT FOUND in primary sources** (the Desktop shell
  is closed-source) and would need black-box verification against the installed app.

## Verdict for the requested feature

A **true no-restart router** — running turn transparently continues on account B after account A hits
its limit — is **not feasible against upstream Codex as-is**:

1. File-swap is invisible until explicit `reload()`; auto-recovery refuses cross-account reloads (§1).
2. `UsageLimitReached` (429 + `usage_limit_reached`) is terminal for the turn with no failover hook;
   only `RateLimitExceeded`-class errors retry, on the same credential (§3).
3. One process = one account; login RPCs swap process-wide identity between turns only (§4).
4. Desktop's backend reads `~/.codex` directly; an outside proxy only sees traffic the config sends
   it, and Desktop-shell behavior is unverifiable from open source (§5).

What *is* feasible without a restart (between turns, not mid-turn):

- **A. Process-wide swap between turns** via app-server `account/*` login RPCs (`reload()` /
  `set_external_auth`), accepting that the in-flight turn fails and the next turn uses the new
  account.
- **B. N concurrent `codex app-server --stdio` processes with distinct `CODEX_HOME`s** (one login
  each — never copies of one `auth.json` per #15410), routing *new* threads to a healthy process.
  Running threads stay on their home; nothing migrates mid-turn.
- **C. Local proxy via `openai_base_url`** (or custom provider + `requires_openai_auth`) that maps
  `usage_limit_reached` itself and retries upstream under another credential. Works cleanly for
  API-key providers; brittle for ChatGPT OAuth (single-use refresh, account pinning, fail-closed
  pinned consumers).

Recommendation: keep the current restart-based switcher as the correctness baseline; if a no-restart
mode is pursued, scope it to **B (route new work to a pre-warmed second home)** and surface
"switch applies to the next turn" honestly — mid-turn continuation contradicts upstream's terminal
`UsageLimitReached` handling.
