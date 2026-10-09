# Product decisions

## 1. Product principle

Sidekick is a personal Codex companion. Every persistent control must directly support one of four jobs:

1. inspect account Usage;
2. select an account;
3. maintain the saved account list;
4. exit the application.

Personal fork: no auto-update. Updates are `git pull` plus a local build. Other product expansion remains outside the MVP.

## 2. Expanded island screen

The notch island is the only interface. Its expanded screen uses the physical notch geometry when available and a floating capsule on other Macs. One scrollable list contains every saved account, with compact per-account Usage rows and reset-credit counts when available. The panel height follows the account count within the available screen space. Add Account and Quit stay in a visible footer while large lists scroll.

Each saved account row shows the full saved email as its title, falling back to the display name if the email is missing or empty. Compact labeled rows show the available 5-hour and weekly Usage percentages and reset countdowns. If Codex has no exact 5-hour window, only the weekly row appears.

The current account row is highlighted.

The visible footer contains:

- `Add Account` on the left, with no inline browser sign-in hint; while sign-in is pending, it becomes a `Cancel Sign-In` action with a compact progress indicator.
- `Quit` on the right, also available through Command-Q.

Each account with a saved reset credit also has a neutral capsule button beside it, showing only the reset icon and available-credit count. Its help and confirmation show the available expiry dates. Confirmation says it consumes one credit, immediately refreshes eligible usage windows, may move the weekly reset date, and makes no purchase.

Removal is available from the context menu of an inactive account row and uses a native confirmation dialog. Cancel performs no deletion.

Quit directly terminates the application, remains available during mutations, and has a Command-Q equivalent.

## 3. Optional 5-hour Usage with weekly Usage

The product always displays the weekly Codex allowance. It also displays an exact 5-hour allowance when Codex provides one.

The normalizer accepts only `windowDurationMins == 300` as the 5-hour window. Four-hour and six-hour windows remain unrelated and are ignored for this field. The app parses and caches the optional 5-hour fields whenever Codex returns them.

The displayed percentage is remaining allowance:

```text
remainingPercent = clamp(100 - window.usedPercent, 0, 100)
```

Each progress-bar width and text use the same `remainingPercent` value.

The last successful weekly value and optional 5-hour value are persisted per account. Older weekly-only cache entries remain decodable. App launch and manual refresh make direct HTTP usage and reset-inventory requests per account. Both usage windows are normalized from the usage response. No CLI fallback or automatic token renewal is used; failed requests preserve the previous complete snapshot. Each trigger replaces the pending background timer with one scheduled for 30 seconds after that trigger. Cached Usage remains visible while refresh runs, and a successful response replaces both display and cache. A refresh failure keeps cached Usage with a warning; without cached data the row shows `Usage unavailable`. Overlapping triggers share the active refresh round.

The application does not substitute another quota window.

## 4. Switching confirmation

Selecting another account opens a native confirmation dialog. It explains fixed behavior rather than exposing configuration switches:

- Codex Desktop will close and reopen;
- a Desktop task that is currently running may stop;
- existing Codex CLI processes are not restarted and continue with the account state they already loaded;
- newly started Codex CLI processes use the newly selected account.

These are product semantics, not Settings options.

The confirmation uses a native macOS dialog. Cancel keeps the island open and starts no switch operation.

## 5. Direct switching flow

The switch implementation is intentionally sequential:

```text
Preflight target and original active profile
→ close Codex Desktop
→ save current credentials
→ activate target credentials
→ verify target identity
→ commit active profile
→ reopen Codex Desktop
```

Before any credential write, preflight reads the registry and validates `originalActiveID`. After target activation succeeds, a verification or registry-commit failure restores `~/.codex/auth.json` from the original profile snapshot saved earlier in the same attempt. An activation failure does not run restoration because replacement did not complete. A Desktop-reopen failure occurs after the registry commit and keeps the selected account active.

The MVP does not implement:

- a general rollback state machine;
- credential backup files;
- a transaction journal;
- automatic retries;
- startup recovery;
- silent fallback to the previous account.

When a step fails, execution stops and the exact original error is shown. If the bounded credential restoration also fails, the same error report contains both failures.

## 6. Credential storage

The MVP uses ordinary local files with user-only permissions.

- Codex active credential: `~/.codex/auth.json`
- Sidekick registry: `~/Library/Application Support/Sidekick/accounts.json`
- Usage cache: `~/Library/Application Support/Sidekick/usage-cache.json`
- Saved account credential: `~/Library/Application Support/Sidekick/accounts/<profile-id>/auth.json`

macOS Keychain is not used in the MVP. It adds implementation complexity without adding user-visible switching value.

## 7. Account lifecycle

The main screen owns account lifecycle operations:

- add account through browser sign-in;
- remove an inactive account.

The active account cannot be removed because the meaning of the active Codex authentication file would otherwise be ambiguous. The user first selects another account and then removes the old one.

Removing an account deletes only Sidekick's local profile directory and metadata. It does not claim to revoke every OpenAI server session.

## 8. Main screen

The expanded island screen shows saved accounts, their 5-hour and weekly Usage, and account controls together. The compact island shows the weekly percentage when the menu-bar clearance check finds room beside the notch.

Add and remove actions live on the same screen as Usage; there is no separate manage page.

The app does not register a macOS Login Item and cannot start itself at login. Users who want it to launch automatically add it themselves in System Settings → General → Login Items.

The following controls are intentionally absent:

- restart behavior;
- CLI behavior;
- confirmation behavior;
- credential storage selection;
- Keychain status;
- Usage-window selection;
- automatic rotation.

## 9. Architecture

The production client is a native SwiftUI app whose only interface is an AppKit panel anchored to the notch or floating capsule.

It does not require:

- Electron;
- a local HTTP server;
- a daemon;
- a reverse proxy;
- a cloud service;
- modification of Codex Desktop;
- injection into Codex UI.

The HTML prototype is a visual reference, not the production runtime.

## 10. Error philosophy

Errors must be visible and attributable to a specific step.

The implementation should:

- use small typed operations;
- stop at the first failed operation;
- preserve the original system error where useful;
- avoid broad `catch` blocks that convert every failure into a generic message;
- avoid retry loops unless a later product requirement explicitly adds them.

A partially completed switch remains observable. The implementation performs only the documented pre-commit credential restoration and adds no generic repair, retry, or startup-recovery behavior.
