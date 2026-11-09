# Testing

## 1. Philosophy

The MVP test suite should prove two things:

1. the direct happy path works;
2. failures stop exactly where they occur and remain visible while the bounded pre-commit credential restoration preserves account consistency.

It should test the documented verification/commit restoration and confirm that no general rollback state machine, retry, credential backup file, journal, or startup recovery exists.

### Local toolchains

Xcode uses SwiftPM's normal `Testing` module discovery. Some standalone Command Line Tools distributions place `Testing.framework` and `lib_TestingInterop.dylib` outside SwiftPM's default search paths. For that CLT layout, `Package.swift` derives the selected toolchain root from `xcrun --find swift` and adds only the required framework and runtime search paths when both files exist. The repository contains no fixed developer-directory path.

If `swift test` prints only `Build complete!`, the generated test entry point also needs the framework search path. On that CLT layout, run:

```sh
toolchain_root=$(dirname "$(dirname "$(dirname "$(xcrun --find swift)")")")
swift test -Xswiftc -F -Xswiftc "$toolchain_root/Library/Developer/Frameworks"
```

Require the `Test run with ... tests ... passed` summary before treating the suite as executed.

## 2. Unit tests

### 2.1 Usage windows

Test:

- `used=0` → `100% left`;
- `used=24` → `76% left`;
- `used=58` → `42% left`;
- `used=100` → `0% left`;
- values below 0 and above 100 are clamped;
- a six-to-eight-day window is selected from either `primary` or `secondary` buckets;
- exactly 300 minutes is accepted as the optional 5-hour window;
- 4-hour and 6-hour windows are not labeled as 5-hour Usage;
- absence of a six-to-eight-day window returns `Usage unavailable`;
- absence of a 5-hour window preserves valid weekly Usage;
- weekly-only cache entries from older versions still decode.

### 2.2 Profile repository

Test:

- load empty repository;
- create profile;
- save active credential into profile;
- activate profile credential;
- delete inactive profile;
- reject deleting active profile;
- persist normalized Usage with mode `0600`;
- remove cached Usage with its inactive profile;
- surface read, write, atomic replacement, and delete errors.

### 2.3 Identity matching

Test:

- matching account IDs succeed;
- differing account IDs fail;
- normalized matching emails succeed when account ID is unavailable;
- differing emails fail;
- missing ID and email fails.

## 3. Switch-flow tests

Use fakes for Desktop, account storage, and Codex identity.

Expected call order:

```text
closeDesktop
saveCurrent
activateTarget
readActiveIdentity
commitActiveAccountID
openDesktop
```

Inject an error at each call and assert:

- the error reports the correct `SwitchStage`;
- no later call occurs;
- no retry occurs;
- `isMutating` returns to false after presentation.

Specific partial-state assertions:

- the registry is read and `originalActiveID` is validated before saving or replacing credentials;
- activation error before replacement leaves state metadata unchanged and runs no restoration;
- verification error restores the original active credential and leaves the original metadata active;
- state-write error restores the original active credential and leaves the original metadata active;
- a retry after restoration cannot overwrite the original profile with target credentials;
- a restoration failure reports both the original and restoration errors;
- Desktop-open error leaves target auth and target metadata active.

Use both the fake store and the real `AccountStore` so call ordering, atomic credential installation, registry-write failure, and on-disk bytes are covered.

## 4. App-server fixture tests

Create a fake JSON-RPC child process that returns:

- identity success;
- identity mismatch;
- 5-hour and weekly rate-limit success from one response;
- response with both `primary` and `secondary`;
- response with `primary` only;
- malformed JSON;
- process exit;
- request timeout.

Assert that the client does not retry, makes one rate-limit request per read, and does not convert one duration into another Usage window.

`CodexExecutableLocatorTests` starts real sh, bash, and zsh login shells with isolated startup files.
Each shell covers unset and empty `CODEX_CLI_PATH`, shell-local command and absolute-path overrides,
invalid overrides, startup output, paths containing spaces, and PATH inheritance by the launched child.

## 5. UI tests

Verify:

- every saved account appears in the expanded island list, with compact per-account Usage rows and available reset-credit counts;
- the panel height follows the account count within the screen bounds, while Add Account and Quit remain visible and long lists scroll;
- current row highlight spans the account and reset action;
- account rows use the full saved email, including its domain, as the primary title;
- account titles fall back to the display name when no email is available;
- missing or empty emails use the display name;
- Add Account has no inline hint, and pending sign-in shows Cancel Sign-In with a progress indicator;
- accounts with matching email prefixes and different domains remain distinguishable;
- the current account row is highlighted;
- available reset credits show a count and open a confirmation dialog;
- rows show labeled 5-hour and weekly percentages with reset countdowns;
- reset countdowns tick while the island is expanded, and hovering one reveals the exact reset timestamp;
- the 5h row is omitted when data is absent;
- footer shows Add Account and Quit side by side;
- switch, removal, and reset confirmations use native macOS dialogs;
- switch confirmation describes Desktop and CLI consequences;
- canceling switch or removal keeps the island open and performs no mutation;
- closing the browser during Add Account leaves the island interactive, and Cancel Sign-In stops the pending login;
- account rows display cached Usage while refresh runs;
- a short-interval timer test proves that a manual refresh postpones the previous deadline and cancellation stops later rounds;
- active profile remove button is disabled;
- the compact island shows weekly percentage only when menu-bar clearance leaves room for its wings;
- the expanded island shows available 5-hour and weekly rows.

## 6. Manual test matrix

With two real test accounts:

1. add Account A;
2. add Account B;
3. expand the island and confirm the weekly Usage row appears for both;
4. confirm available 5-hour data appears in its own row above weekly Usage, and an account without 5-hour data shows only the weekly row;
5. confirm the compact island percentage matches weekly Usage when its wings are visible;
6. start a Codex CLI under A;
7. switch A → B;
8. with an active local chat, leave Desktop's native quit dialog pending and confirm a close-stage error after 30 seconds, with account A still active and Desktop still running; then finish or stop the task and close Desktop normally;
9. switch again and confirm Desktop reopens as B; reopen existing tasks and check their earlier and latest messages;
10. confirm the existing CLI was not terminated;
11. start a new CLI and confirm it uses B;
12. switch B → A;
13. disconnect network and confirm cached Usage remains visible with a warning;
14. remove the cache, disconnect network, and confirm Usage unavailable appears;
15. collapse the island, wait 30 seconds, and confirm `usage-cache.json` receives a newer successful value;
16. force identity mismatch and confirm the original `auth.json` is restored while the mismatch remains visible;
17. make `accounts.json` unwritable and confirm the original `auth.json` is restored while the state-write failure remains visible;
18. make restoration fail and confirm both errors are shown.

## 7. Repository checks

Before release:

```text
only main branch remains
5-hour Usage is always shown on account rows
4-hour and 6-hour windows are never labeled as 5-hour Usage
only the bounded verification/commit credential restoration exists
no general rollback state machine
no credential backup file
no transaction journal
no Keychain implementation
no login-item or launch-at-login support
no account-rename UI or model operation
```

History safety regression tests cover a graceful exit taking more than two seconds, timeout and cancellation, no credential mutation after a close-stage failure, byte preservation of history/index/WAL files through A → B → A, and selection of Desktop's runtime override. These checks do not prove recovery of an already damaged Codex history projection.

## 8. Release automation checks

Personal fork: no release workflow. This section is kept only as history.

## 9. Whole-project ablation and integrity regression

New regression cases cover external-login contamination, successful RPC responses with a wrong target identity, account-list write failure before credential deletion, list restoration after deletion failure, a final JSON response without a newline, unrelated RPC error IDs, subprocess error details, numeric overflow, and Codex quota isolation from other metered products.
