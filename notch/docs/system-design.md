# System design

## 1. Overview

Sidekick is a native macOS app whose only interface is a notch island. It manages multiple local Codex authentication snapshots.

The system has one core operation:

```text
copy the selected profile's auth.json into ~/.codex/auth.json
```

Everything else exists to make that operation understandable and usable:

- identify saved accounts;
- display weekly Usage and optional 5-hour Usage;
- close and reopen Codex Desktop;
- save the currently active credential before replacing it;
- verify that Codex can read the selected identity;
- maintain a small amount of local metadata.

The MVP uses direct sequential code and lets failures surface. One bounded consistency action restores the validated original profile credential when target verification or the registry commit fails after activation. There is no general rollback framework.

## 2. Design goals

### 2.1 Goals

- one-click account selection after one confirmation;
- native macOS behavior;
- no modification to Codex Desktop;
- shared Codex configuration and history under `~/.codex`;
- independent saved authentication profiles;
- weekly Usage and optional exact 5-hour Usage for each profile;
- immediate display of persisted Usage while refresh runs;
- explicit, stage-specific errors;
- code small enough to review as an MVP.

### 2.2 Non-goals

- zero-downtime switching;
- a general rollback state machine for arbitrary partial completion;
- automatic repair;
- background daemon;
- cloud synchronization;
- secure enclave or Keychain integration;
- account routing per request;
- concurrent account use inside one Codex process;
- compatibility abstraction for every future Codex authentication backend.

## 3. Platform and technology

Recommended stack:

- macOS 14+;
- Swift 6;
- SwiftUI for UI;
- SwiftUI for the single expanded island screen and its native confirmation dialogs;
- `NSPanel` for notch geometry, screen placement, and Spaces behavior;
- AppKit for process discovery and application lifecycle;
- Foundation `FileManager`, `Process`, and JSON coding;
- Codex app-server JSON-RPC for sign-in and identity; direct authenticated HTTP for Usage reads.

No third-party runtime dependency is required beyond the installed Codex executable.

## 4. High-level architecture

```mermaid
flowchart LR
    UI[SwiftUI notch island]
    VM[AppModel @MainActor]
    Store[AccountStore actor]
    Switch[SwitchService]
    Codex[CodexClient]
    Desktop[DesktopController]
    FS[Local files]

    UI --> VM
    VM --> Store
    VM --> Switch
    VM --> Codex
    Switch --> Store
    Switch --> Desktop
    Switch --> Codex
    Store --> FS
    Codex --> CLI[Installed codex executable]
```

The application is a single process. There is no daemon and no local network service.

## 5. Source layout

```text
Sources/Sidekick/
├── SidekickApp.swift
├── AppModel.swift
├── IslandAccountManager.swift
├── DynamicIslandPanel.swift
├── VorssaintNotchSupport.swift
├── NotchShape.swift
├── NotchMenuBarSpace.swift
├── AccountRow.swift
└── DesktopController.swift

Sources/SidekickCore/
├── AccountController.swift
├── AccountStore.swift
├── CodexClient.swift
├── CredentialIdentity.swift
├── Models.swift
├── SwitchService.swift
└── WeeklyUsageNormalizer.swift

Tests/SidekickTests/
```

Avoid adding protocol layers until a second implementation actually exists. Small concrete types are preferable for the MVP.

## 6. Local storage

### 6.1 Paths

```text
~/.codex/
└── auth.json                         # active credential used by Codex

~/Library/Application Support/Sidekick/
├── accounts.json                     # profile metadata and active account ID
├── usage-cache.json                  # last successful normalized Usage per profile
└── accounts/
    ├── <profile-id>/
    │   └── auth.json                 # saved credential snapshot
    └── <profile-id>/
        └── auth.json
```

Directory permissions:

```text
Sidekick directory 0700
accounts/<profile-id>                 0700
auth.json                             0600
accounts.json                         0600
usage-cache.json                      0600
```

The implementation sets these permissions when creating files. It does not build a separate permission-audit or repair subsystem.

On first launch, Sidekick moves `~/Library/Application Support/Codex Account Switcher/` to the new data path. If both folders exist, it leaves both unchanged and reports a migration conflict rather than overwrite either set of account data.

### 6.2 `accounts.json`

```json
{
  "activeAccountID": "6FB26BF8-57C2-4DF6-8C2A-B4A5E5E6A915",
  "accounts": [
    {
      "id": "6FB26BF8-57C2-4DF6-8C2A-B4A5E5E6A915",
      "displayName": "Personal",
      "email": "user@example.com",
      "accountID": "acct_123",
      "createdAt": "2026-08-21T06:00:00Z",
      "lastUsedAt": "2026-08-21T06:00:00Z"
    }
  ]
}
```

`accountID` and `email` are metadata returned by Codex when available. The login identity supplies the local display name; the MVP has no rename operation.

### 6.3 Usage cache

`usage-cache.json` stores `profileID`, normalized weekly Usage, optional 5-hour Usage, and `fetchedAt` for each last successful response. The optional fields allow older weekly-only cache entries to decode. No switch phase, rollback reference, or recovery journal is stored.

### 6.4 Writes

Credential activation uses a temp file and rename in the same directory:

```text
copy target auth bytes to ~/.codex/auth.json.sidekick-<uuid>.tmp
→ chmod 0600
→ rename the temporary file to auth.json
```

This is used only to prevent a partially written JSON file. It is not a rollback mechanism.

Metadata can use the same small `write temp → rename` helper. There is no history file and no previous-version retention.

## 7. Domain model

### 7.1 `AccountProfile`

```swift
struct AccountProfile: Codable, Identifiable, Equatable {
    let id: UUID
    var displayName: String
    let email: String?
    let accountID: String?
    let createdAt: Date
    var lastUsedAt: Date?
}
```

### 7.2 `WeeklyUsage`

```swift
struct WeeklyUsage: Codable, Equatable {
    let remainingPercent: Int
    let resetsAt: Date
    let fiveHourRemainingPercent: Int?
    let fiveHourResetsAt: Date?
}
```

The normalizer clamps values:

```swift
remainingPercent = min(100, max(0, round(100 - usedPercent)))
```

### 7.3 Usage cache and view state

```swift
struct UsageCacheEntry: Codable, Equatable {
    let profileID: UUID
    let usage: WeeklyUsage
    let fetchedAt: Date
}

enum UsageViewState: Equatable {
    case idle
    case loaded(WeeklyUsage)
    case stale(WeeklyUsage, String)
    case unavailable(String)
}
```

### 7.4 `SwitchStage`

```swift
enum SwitchStage: String {
    case closeDesktop
    case saveCurrentCredential
    case activateTargetCredential
    case verifyTargetIdentity
    case commitActiveAccountID
    case reopenDesktop
}
```

The stage is held in memory only for progress and error messages.

## 8. Codex executable discovery

`CodexExecutableLocator` resolves the executable for each requested operation.

Lookup order:

1. test-only explicit URL;
2. the login shell's shared `CODEX_CLI_PATH`, resolved through its `PATH` when it is a command name;
3. the login shell's `codex` command when no shared override is set.

The runtime version is never pinned. Each CLI operation resolves the current system command. The same login-shell PATH is passed to app-server so npm launchers can resolve Node. A missing command or invalid explicit path is an error; Sidekick does not silently start another bundled version.

If not found, show one direct error:

```text
Codex CLI was not found. Install Codex and reopen Sidekick.
```

Do not add download automation or multiple package-manager fallbacks in the MVP.

The island joins Vorssaint’s dedicated overlay Space before the panel first appears. This prevents desktop-switch animations from carrying the island with the wallpaper. The macOS Space functions are resolved at runtime; if unavailable, the stationary AppKit window behavior remains. Windows leave the overlay before it is destroyed.

## 9. Codex app-server client

### 9.1 Process

For a given profile home:

```text
CODEX_HOME=<profile-home> codex app-server --stdio
```

The client communicates with newline-delimited JSON-RPC messages over stdin/stdout.

Basic handshake:

```json
{"method":"initialize","id":1,"params":{"clientInfo":{"name":"sidekick","title":"Sidekick","version":"0.1.7"}}}
{"method":"initialized","params":{}}
```

The exact notification envelope should follow the installed Codex schema. Generate or inspect the app-server schema during implementation rather than duplicating unrelated protocol types.

### 9.2 Identity read

Use the account-read RPC exposed by the installed Codex version. Normalize the response to:

```swift
struct CodexIdentity {
    let email: String?
    let accountID: String?
    let plan: String?
}
```

Identity matching rule for the MVP:

1. compare `accountID` when both sides have it;
2. otherwise compare normalized email;
3. if neither is available, verification fails.

There is no fallback to local display name or file hash.

### 9.3 Usage read

Read the saved profile’s access token and account ID, then GET `https://chatgpt.com/backend-api/wham/usage`. Normalize `rate_limit.primary_window` and `secondary_window`, converting `limit_window_seconds` to minutes. There is no CLI fallback or automatic token renewal. Authentication failures require signing in again.

```text
fiveHour = first window where windowDurationMins == 300
weekly = longest window where 8640 <= windowDurationMins <= 11520
```

The client intentionally ignores:

- any short-duration window other than exactly 300 minutes;
- windows outside six through eight days.

GET `https://chatgpt.com/backend-api/wham/rate-limit-reset-credits` with the same account-scoped credentials. Read `available_count` and available `credits` details. If either request fails, retain the previous complete snapshot. The count is authoritative and detail rows may be capped. Cache available credit IDs and optional expiry dates with the usage data so old cache entries remain decodable; the menu only offers redemption when the count is positive.

Normalized output:

```swift
WeeklyUsage(
    remainingPercent: clamp(round(100 - weekly.usedPercent), 0, 100),
    resetsAt: weekly.resetsAt,
    fiveHourRemainingPercent: fiveHour.map { clamp(round(100 - $0.usedPercent), 0, 100) },
    fiveHourResetsAt: fiveHour?.resetsAt,
    availableResetCount: inventory.available_count
)
```

Optional available reset-credit IDs and expiry dates are carried alongside that normalized usage value when Codex returns detail rows.

If no six-to-eight-day window exists, return `weeklyUsageUnavailable`. Missing 5-hour data leaves the optional fields empty. A 4-hour or 6-hour window never populates them. Both windows come from the same HTTP usage response.

### 9.4 Timeouts

Use one fixed 20-second process/request timeout. Login completion has a separate 10-minute timeout. On timeout:

- terminate the app-server child;
- return a timeout error;
- do not retry.

This prevents a blocked row refresh from hanging indefinitely without creating a retry framework.

### 9.5 Banked reset redemption

After the user confirms, start a Codex app-server in the selected account's profile home, read `account/rateLimits/read` again, and fail closed if reset-credit availability is missing or zero. Redeem one existing credit using `account/rateLimitResetCredit/consume` with a fresh `idempotencyKey` and the earliest-expiring returned `creditId`; omit the ID when details are unavailable so Codex selects a credit. The action never uses a paid-reset purchase path. On completion, refresh usage for every saved account.

## 10. Usage service

### 10.1 Profile-specific `CODEX_HOME`

Each account directory acts as a minimal Codex home for identity and Usage calls:

```text
~/Library/Application Support/Sidekick/accounts/<profile-id>/auth.json
```

Sidekick reads that directory’s saved credentials for HTTP Usage requests. Sign-in and identity CLI calls use it as `CODEX_HOME`. Usage requests do not change the active `~/.codex/auth.json`.

### 10.2 Refresh behavior

At application launch, when the pending 30-second timer fires, and when the user manually refreshes:

1. load account metadata and persisted weekly and optional 5-hour Usage;
2. render cached values immediately without replacing them with a loading state;
3. refresh all accounts concurrently;
4. replace the pending timer with one scheduled 30 seconds after the latest trigger;
5. share one active refresh task when launch, timer, and manual refresh triggers overlap;
6. publish and persist each successful result as it completes;
7. retain cached Usage with a warning on failure, or show unavailable when no cache exists.

The app owns one pending timer while it runs. Every launch, timer, or manual refresh cancels the previous timer and schedules the next deadline 30 seconds later. There is no helper daemon, exponential backoff, or silent retry.

### 10.3 Persisting refreshed credentials

Routine Usage reads never renew or write tokens. Codex may refresh the profile's token during sign-in or other CLI operations. Because that app-server uses the profile directory directly, any updated `auth.json` remains in the correct profile directory. No extra copy-back stage is required.

## 11. Desktop controller

`CodexDesktopController` has two operations:

```swift
func close() async throws
func open() async throws
```

### 11.1 Close

Use `NSRunningApplication` for the Codex Desktop bundle identifier and call `terminate()`.

Codex Desktop can display a quit confirmation while work is active. Allow up to 30 seconds for its normal exit, including its history and settings flush. Never force-terminate Desktop. If the quit request is rejected or Desktop remains running, report a close-stage error before saving or replacing credentials. The user can finish or stop active tasks, close Desktop, and switch again. Cancellation also stops the wait.

Sidekick only observes the Desktop application's exit; it does not terminate CLI processes or claim to repair Codex's history database. Account RPCs read the login shell's `PATH` and shared `CODEX_CLI_PATH` setting. A bare command such as `codex` resolves through that PATH; an explicit absolute path must be executable. The child receives the same PATH so npm launchers can find Node. There is no Sidekick-specific override or automatic selection of another bundled CLI.

If Desktop is not running, `close()` succeeds immediately.

### 11.2 Open

Use `NSWorkspace.shared.openApplication` with the installed Codex application URL.

If launch fails, return the AppKit error. The account switch remains at whatever stage already completed; no automatic account restoration occurs.

### 11.3 CLI processes

Sidekick never enumerates, terminates, signals, or restarts `codex` CLI processes.

An existing CLI process may have already loaded credentials into memory. Therefore:

- existing CLI processes continue independently;
- a new CLI process reads the newly active `~/.codex/auth.json`.

## 12. Direct switch sequence

### 12.1 State diagram

```mermaid
stateDiagram-v2
    [*] --> Preflight
    Preflight --> ClosingDesktop
    ClosingDesktop --> SavingCurrent
    SavingCurrent --> ActivatingTarget
    ActivatingTarget --> VerifyingTarget
    VerifyingTarget --> CommittingProfile
    CommittingProfile --> ReopeningDesktop
    ReopeningDesktop --> Completed

    Preflight --> Failed
    ClosingDesktop --> Failed
    SavingCurrent --> Failed
    ActivatingTarget --> Failed
    VerifyingTarget --> RestoringOriginal
    CommittingProfile --> RestoringOriginal
    RestoringOriginal --> Failed
    ReopeningDesktop --> Failed

    Completed --> [*]
    Failed --> [*]
```

`RestoringOriginal` is the single bounded consistency action after successful target activation and before a successful registry commit. There is no persisted or general rollback state machine.

### 12.2 Pseudocode

```swift
func switchAccount(to targetID: UUID) async throws {
    let target = try store.profile(id: targetID)
    let registry = try store.loadRegistry()
    guard let originalActiveID = registry.activeAccountID,
          registry.accounts.contains(where: { $0.id == originalActiveID }) else {
        throw AccountStoreError.activeProfileMissing
    }

    stage = .closeDesktop
    try await desktop.closeDesktop()

    stage = .saveCurrentCredential
    try await store.saveCurrentCredential()

    stage = .activateTargetCredential
    try await store.activateTargetCredential(id: target.id)

    stage = .verifyTargetIdentity
    do {
        let identity = try await codex.readIdentity(profileHome: store.activeCodexHome())
        guard identity.matches(target) else { throw CodexClientError.identityUnavailable }
    } catch {
        throw await restoreOriginalCredential(originalActiveID, preserving: error)
    }

    stage = .commitActiveAccountID
    do {
        try await store.commitActiveAccountID(target.id)
    } catch {
        throw await restoreOriginalCredential(originalActiveID, preserving: error)
    }

    stage = .reopenDesktop
    try await desktop.reopenDesktop()
}
```

The restoration helper reuses the saved profile credential and the same atomic installation path. It returns the original stage error after a successful restoration, or one error containing both the original and restoration failures.

### 12.3 Preflight

Preflight performs only the minimum required to start:

- target profile exists;
- when `originalActiveID` exists, it refers to a registry profile before credential writes; with no active ID, the shared auth file must also be absent;
- no in-process switch is currently running.

It does not inspect every filesystem property, create backups, test network reachability, or pre-verify credentials.

These checks occur before the six recorded `SwitchStage` values.

### 12.4 Close Codex Desktop

Closing Desktop comes before saving the active credential so Codex has a chance to finish its normal shutdown writes.

### 12.5 Save current credentials

The registry must identify one active account. After Desktop exits, read the shared home’s current identity and match it against this profile before saving. An external login mismatch stops visibly and leaves saved profiles unchanged.

```text
copy ~/.codex/auth.json
→ accounts/<activeAccountID>/auth.json.sidekick-<uuid>.tmp
→ rename to accounts/<activeAccountID>/auth.json
```

If the active file is missing or unreadable, stop and show the error. Do not continue by assuming the stored snapshot is good enough.

### 12.6 Activate target

```text
copy accounts/<targetID>/auth.json
→ ~/.codex/auth.json.sidekick-<uuid>.tmp
→ chmod 0600
→ rename the temporary file to ~/.codex/auth.json
```

If activation fails, stop. Atomic installation throws before a completed replacement, so no restoration runs. There is no credential backup file.

### 12.7 Verify target

Start Codex app-server using the active `~/.codex` home and read identity.

If identity does not match the target metadata, preserve the mismatch as the stage error and reinstall the just-saved original profile credential into `~/.codex/auth.json`.

### 12.8 Commit active profile

After successful identity verification, write `activeAccountID` and `lastUsedAt` to `accounts.json`.

If this write fails, preserve the registry error and reinstall the just-saved original profile credential. The registry continues to name the original profile. No retry or startup reconciliation runs.

### 12.9 Reopen Desktop

Always attempt to reopen Codex Desktop after metadata commit.

If opening fails, report `reopeningDesktop` failure. The selected account remains active.

## 13. Failure behavior

### 13.1 Error model

```swift
struct SwitchFailure: LocalizedError {
    let stage: SwitchStage
    let underlying: Error
}
```

Example presentation:

```text
Could not verify selected account.

Stage: Verifying selected account
Error: Codex returned user@example.com, expected work@example.com.
```

After a successful restoration, keep this original verification error visible. Do not replace it with a success-style message such as:

```text
Something went wrong, but everything is fine now.
```

If restoration also fails, append that failure to the original message and retain both underlying diagnostic descriptions.

### 13.2 No hidden recovery

The following are intentionally absent:

- restoration outside the documented verification/commit failure window;
- a general rollback state machine;
- credential backup files;
- retry with another profile;
- retry with cached identity;
- alternate rate-limit window;
- startup journal replay;
- delayed cleanup queue;
- periodic consistency checker.

### 13.3 Manual resolution

The user can resolve a failed state by performing another explicit action:

- select the desired account again;
- add or re-login the profile again;
- open Codex Desktop manually;
- inspect the reported path or Codex error.

Manual resolution is not triggered automatically by the app.

## 14. Add-account flow

### 14.1 Sequence

```mermaid
sequenceDiagram
    actor User
    participant UI
    participant Store as AccountStore
    participant Codex as Codex Login

    User->>UI: Add Account
    UI->>Store: create profile directory
    UI->>Codex: run login with profile CODEX_HOME
    Codex-->>User: browser/device login
    Codex-->>UI: login completes
    UI->>Codex: read identity
    Codex-->>UI: account metadata
    UI->>Store: append profile
    UI-->>User: account appears
```

### 14.2 Details

1. Generate a profile UUID.
2. Create `accounts/<uuid>/`.
3. Run Codex login with `CODEX_HOME` set to that directory.
4. Wait for login completion.
5. Read identity from the same profile home.
6. Derive the local display name from the returned identity.
7. Append profile metadata.
8. Return to the account list.

If any step fails, stop and show the error. Remove this login attempt’s directory only while it remains unregistered. Preserve the original error; surface cleanup failure too. Cancellation stops the login session and performs the same cleanup.

## 15. Remove-account flow

1. Reject the active or an unknown profile.
2. Remove its cached Usage if present.
3. Persist the account list without the removed profile.
4. Delete the profile directory.
5. If directory deletion fails, restore the original account list and report the original error. Report both errors if list restoration also fails.

A registry-write failure leaves the credential directory intact. The bounded list restoration covers the demonstrated deletion failure; it does not make recursive deletion crash-atomic or restore a partially deleted directory. No tombstone, retry queue, journal, or credential copy is introduced.

## 16. UI state management

`AppModel` is `@MainActor` and owns:

```swift
@Published private(set) var accounts: [AccountProfile] = []
@Published private(set) var activeAccountID: UUID?
@Published private(set) var usageStates: [UUID: UsageViewState] = [:]
@Published private(set) var isMutating = false
@Published private(set) var isAddingAccount = false
@Published var visibleError: OperationError?
```

Only one mutation runs at a time. While `isMutating` is true:

- account rows are disabled;
- account lifecycle actions that mutate profiles are disabled;
- Quit remains available.

This is a simple in-process UI invariant, not a cross-process lock service.

Browser sign-in uses `isAddingAccount` and a separately retained task. It does not hold the mutation lock while waiting for the browser callback. The main screen replaces Add Account with a cancel action during this wait; canceling terminates the app-server session and restores the normal button without showing an error.

`IslandAccountManager` shows all saved accounts in one scrollable list, with compact per-account Usage rows and reset-credit counts when available. The expanded panel grows with the account count up to the available screen height. Its Add Account and Quit footer stays visible while the list scrolls. Switch, reset, and removal use native confirmation dialogs. Cancel performs no mutation.

## 17. Product copy

Product copy lives as inline English string literals at the call site. There is one user and one language, so no key table or localization layer exists.

## 18. Observability

The current MVP surfaces operation failures directly in the island and retains the underlying diagnostic description in memory. It does not log auth file contents or access tokens.

There is no remote telemetry, support bundle, analytics pipeline, or automatic diagnostics upload.

## 19. Compatibility assumptions

The implementation assumes:

- Codex stores active file credentials under `CODEX_HOME/auth.json` when file credential storage is used;
- the installed Codex app-server exposes account identity and rate-limit RPCs;
- 5-hour Codex windows report exactly 300 minutes when available;
- weekly Codex windows report a duration between six and eight days;
- Codex Desktop reads the active `~/.codex` authentication state when launched.

These assumptions should be checked against the Codex version used during implementation. The MVP does not add a compatibility adapter matrix. Unsupported versions fail with a direct message.

## 20. What must not be added during MVP

Do not add the following unless a new product decision explicitly requires it:

- rollback copy;
- transaction journal;
- crash recovery coordinator;
- retry policy;
- failover;
- Keychain abstraction;
- SQLite;
- daemon;
- helper process that stays resident;
- separate Usage or account-management screens;
- switch-behavior settings;
- CLI process control;
- automatic account selection.

## 20. v0.1.7 lifecycle completion

First activation skips saving an original credential when both active ID and shared auth are absent; recheck file absence after Desktop closes. If verification or commit fails, clear only the newly installed active auth and preserve the saved target profile. Normal switches retain the existing original-credential restoration path.

AccountStore rejects duplicate identities on add. Login cleanup checks the registry before deleting the attempt directory. The app bundle's `CFBundleShortVersionString` and `CFBundleVersion` both come from `RELEASE_VERSION` in `scripts/package-local-app.sh` (default `0.1.17`), so there is no separately maintained build number.
