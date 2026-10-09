# Product requirements

## 1. Goal

Allow an ordinary Mac user with multiple authorized ChatGPT accounts to add them through browser sign-in and choose which account Codex Desktop uses from the menu bar, without Terminal commands or config-file editing.

The user should understand the product after opening the menu once. The main path should require no configuration.

## 2. Scope

### In scope

- macOS menu-bar application;
- saved local account profiles;
- one weekly Usage value and an optional exact 5-hour Usage value per account;
- reset countdowns for available Usage windows;
- account switching;
- add account;
- remove inactive account;
- English UI.

### Out of scope

- quota-window selector;
- automatic rotation;
- failover;
- round-robin routing;
- account recommendation;
- automatic switching when Usage is low;
- general rollback and recovery state machines;
- server-side session revocation;
- syncing profiles between Macs.

## 3. Primary user stories

### US-1: Inspect accounts

As a user, I can open the menu-bar popover and immediately see saved accounts, weekly Usage, and a reset countdown. I can optionally see a 5-hour row.

### US-2: Switch accounts

As a user, I can select a different account, read the normal consequences, confirm once, and have Codex Desktop reopen under that account.

### US-3: Add an account

As a user, I can add another ChatGPT account through Codex login from the main screen, and save it as a selectable profile.

### US-4: Remove an account

As a user, I can remove an inactive local profile from the account row’s context menu.

## 4. Menu-bar trigger

The menu-bar item uses a compact account-group icon with an accessibility label. It does not wait for account or Usage data before appearing.

The popover content width is 300 points.

## 5. Account row

### 5.1 Layout

Each row keeps the compact layout:

```text
Email Address
[================----]     42%   Resets in 3d 4h
```

Constraints:

- do not show a per-account avatar or initials; keep the bars compact and use the freed horizontal space to make the percentages and countdowns easier to scan;
- use the full saved email as the title, falling back to the display name when email is missing or empty;
- keep the title on one line with tail ellipsis and show its full text in a floating tooltip on hover;
- each Usage line ends with its reset countdown;
- show no separate visible `5h`/`7d` label column; the 5-hour row appears above the weekly row;
- the percentage is remaining percentage;
- the progress bar width equals the displayed percentage;
- the current account is highlighted across the full row, including its reset action when present;
- no checkmark is shown;
- no `Current` text is shown.

With exact 5-hour data available, the row expands:

```text
Email Address
[==========------]        65%   Resets in 2h 14m
[========--------]         42%   Resets in 3d 4h
```

The 5-hour line is first and is omitted when its data is absent; the weekly line remains. The UI does not show a visible window-label column or an unavailable placeholder for the missing 5-hour line. Usage bars are short visual indicators; the percentage text carries the value.

When an account has one or more banked reset credits, show a neutral capsule button beside the account with the reset icon and available-credit count only; do not repeat `Reset` as visible text. The active-row highlight spans behind both the account and reset action. Expose the available expiry dates in the button's help and confirmation. It redeems one existing credit only; it never opens a paid checkout. Require an in-popover confirmation that explains the eligible usage windows refresh immediately, the next weekly reset date may move, and the credit cannot be restored. The action uses that saved account's Codex profile, not whichever account happens to be active.

### 5.2 Usage-window normalization

Input from Codex may include more than one rate-limit window. One `account/rateLimits/read` response supplies both normalized windows.

Select `rateLimitsByLimitId["codex"]` when present, otherwise the legacy top-level `rateLimits`. Read only that bucket’s `primary` and `secondary` windows, then choose the longest window whose duration is six through eight days. Other metered products must not supply Codex Usage:

```text
fiveHour = first(windows where windowDurationMins == 300)
weekly = max(windows where 8640 <= windowDurationMins <= 11520)
```

If no six-to-eight-day window exists:

```text
Usage unavailable
```

The UI must not infer a weekly value from a short window, regardless of whether Codex labels it `primary` or `secondary`. Four-hour and six-hour windows must not be labeled as five-hour Usage. Missing 5-hour data preserves valid weekly Usage.

### 5.3 Percentage

```text
usedPercent = window.usedPercent
remainingPercent = clamp(100 - usedPercent, 0, 100)
```

Examples:

| `usedPercent` | UI text | Bar width |
| ---: | ---: | ---: |
| 0 | `100% left` | 100% |
| 24 | `76% left` | 76% |
| 58 | `42% left` | 42% |
| 100 | `0% left` | 0% |

### 5.4 Reset formatting

Each Usage line shows how long until that window resets, so the popover answers "when can I code again" without mental math:

```text
Resets in 2h 14m
Resets in 3d 4h
Resets in 45m
Resets in a moment
Resets now
```

The countdown ticks while the popover is open. Hovering the countdown reveals the exact reset timestamp, with the weekday-level date included for the weekly window and omitted for the 5-hour window. An unavailable Usage window shows the unavailable state instead of a reset placeholder.

## 6. Main-menu footer

The footer is a single horizontal row with two equal-width actions:

1. `Add Account` on the left, without an inline browser sign-in hint; while sign-in is pending, it becomes `Cancel Sign-In` with a compact progress indicator.
2. `Quit` on the right.

Both are ordinary in-line buttons; they do not replace the popover content and provide no Back action.

Quit directly terminates the application, remains enabled while another operation runs, and is also available through Command-Q.

## 7. Switching flow

### 7.1 Selecting current account

Selecting the highlighted account closes the popover or does nothing. It does not restart Codex and does not show confirmation.

### 7.2 Selecting another account

Show a confirmation page inside the popover with this meaning:

> Switch to **{account name}**?
>
> Codex Desktop will close and reopen. A task currently running in Desktop may stop. Existing Codex CLI sessions continue with the account they started with. New CLI sessions use the selected account.

Actions:

- `Cancel`
- `Switch Account`

Cancel returns to the account list without closing the popover or starting the switch.

### 7.3 Progress

After confirmation, disable additional account actions while the six-stage operation runs:

- `Closing Codex Desktop…`
- `Saving current account…` (read and match the active identity before saving its credential)
- `Activating selected account…`
- `Verifying selected account…`
- `Saving selected account…`
- `Opening Codex Desktop…`

If Codex Desktop displays its quit warning for active local chats, the user handles that native dialog. Sidekick waits up to 30 seconds for normal exit and never force-quits Desktop. A rejected quit request or timeout stops the switch before credentials change, preserving Desktop's opportunity to flush task history. The user can finish or stop tasks and close Desktop before switching again.

The stages are not configurable. A failure banner identifies the failed stage and retains the underlying diagnostic message.

### 7.4 Success

On success:

- the selected row becomes highlighted;
- `activeAccountID` is updated;
- Codex Desktop is reopened;
- the popover remains available on the account list.

### 7.5 Failure

On failure:

- stop immediately;
- do not continue to later stages;
- show the failed stage and the underlying error;
- keep the error visible until the user dismisses it.

If target activation completed and target verification or registry persistence fails, restore the active `auth.json` from the validated original profile saved earlier in the attempt. Keep the original failure visible. If restoration fails, show both the original and restoration errors. Do not restore after an activation failure that did not replace the credential, and keep the target account active when Desktop reopening fails after a successful registry commit.

This bounded repair does not add a general rollback state machine, credential backup file, retry, journal, or startup recovery.

## 8. Account lifecycle

### 8.1 List

The main screen lists every saved account:

- no per-account avatar or initials; use the freed horizontal space for a cleaner scan of the account title and Usage rows;
- full email as a single-line title with tail ellipsis and a floating full-text tooltip;
- local display name when email is missing or empty;
- active-state highlight;
- remove action for inactive profiles in the row’s context menu.

### 8.2 Add account

`Add Account` starts a Codex login using a new profile directory.

There is no inline browser sign-in hint beside `Add Account`.

Expected flow:

```text
create profile directory
→ run Codex login with CODEX_HOME=<profile directory>
→ read account identity
→ create profile metadata
→ return to the account list
```

Adding an account creates no current-account credential backup or rollback flow.

The browser sign-in wait is tracked separately from profile mutations so the account list and existing account actions remain interactive. While sign-in is pending, the `Add Account` button becomes `Cancel Sign-In` and shows a compact progress indicator. Canceling stops the app-server login session without presenting an operation error.

If login fails, show the original Codex login error and remove only this attempt’s unregistered profile directory. Cancellation performs the same cleanup without a login error. If cleanup fails, expose that failure alongside the original error. Already registered profiles are never removed by this cleanup. Duplicate identities are rejected; distinct account IDs remain separate even when their emails match.

### 8.3 Remove

Removing an inactive profile:

```text
persist removal from profile metadata
→ delete accounts/<profile-id>/
```

If metadata persistence fails, leave the credential directory intact. If directory deletion fails, restore the original account list and show the filesystem error; if this restoration also fails, show both errors. This does not guarantee atomic recursive deletion or process-crash recovery. Do not add a tombstone or deferred cleanup queue.

The active profile's remove control is disabled.

Remove first opens a confirmation page inside the popover. Cancel and the header Back action return to the account list without closing the popover or calling the remove operation.

## 9. Native controls

- account rows are keyboard-focusable buttons;
- controls use their standard macOS button behavior and labels;
- usage bars are decorative; the adjacent percentage text carries the value;
- text remains readable at standard macOS accessibility text sizes.

## 10. Performance

- popover should appear immediately from local metadata;
- persisted weekly and optional 5-hour Usage is rendered before refresh subprocesses complete;
- Usage refresh begins at application launch and whenever the popover opens;
- every refresh trigger replaces the pending timer with a refresh scheduled five minutes after that trigger;
- account refreshes run concurrently and overlapping triggers share the active refresh round;
- one app-owned timer remains active while the popover is closed;
- no automatic retry runs after an individual request fails;
- one failed account does not prevent other rows from rendering;
- switch actions are disabled while one switch is in progress.

## 11. Acceptance criteria

The MVP is accepted when:

1. three saved accounts can be displayed without per-account avatars or initials, with the freed horizontal space used for a cleaner scan of account titles and Usage rows;
2. each row shows its weekly usage line with a compact visual bar, percentage, and reset countdown;
3. when both values exist, rows show an unlabeled 5-hour line above the weekly line, and omit the 5-hour line when its data is absent;
4. the progress bar exactly matches `NN% left`;
5. the current row is indicated by its highlight alone, without a checkmark or `Current` label;
6. the footer places Add Account and Quit side by side, with Command-Q also quitting the app;
7. the popover is 300 points wide and all secondary pages navigate inside it;
8. the main screen shows Add Account without an inline sign-in hint, pending sign-in shows Cancel Sign-In with a compact progress indicator, and an inactive row’s context menu offers Remove;
9. every popover opening starts on the account list;
10. canceling switch or removal performs no mutation and keeps the popover open;
11. Quit and Command-Q terminate the application and remain available during mutations;
12. account switching follows the documented six-stage sequence;
13. a failure at any switch stage stops and is shown directly;
14. verification and registry-commit failures restore the validated original credential, with no general rollback state machine, backup file, journal, retry, or startup-recovery path;
15. existing CLI processes are not terminated;
16. newly started CLI processes observe the selected active `auth.json`;
17. cached Usage stays visible during refresh and is replaced after success;
18. closing the popover does not stop the pending five-minute background cache refresh;
19. inactive accounts can be added and removed; active accounts cannot be removed;
20. only `main` is required for the repository's steady state.

## 12. Sidekick updates

Personal fork: no auto-update. Updates are `git pull` plus a local build.

## 13. First activation and external sign-in

With no registered active account and no active auth.json, selecting a saved account activates it directly after Desktop exits normally. Verification or registry persistence failure removes the newly installed active credential and leaves the app signed out. Existing unregistered credentials are never overwritten by this path.

With no saved accounts and an active `auth.json`, the first launch imports that login once. After that, the app never re-imports an external login: an account signed in outside the app is added again with `Add Account`. Existing unregistered credentials are never overwritten by this path.
