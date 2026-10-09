# Documentation

Personal fork notes. Public release, website, and auto-update docs were removed.

## Documents

| Document | Purpose |
| --- | --- |
| [Product decisions](product-decisions.md) | Binding product and architecture decisions |
| [System design](system-design.md) | Components, storage, flows, interfaces, and failure behavior |
| [Testing](testing.md) | Small, failure-oriented MVP test plan |

## Product definition

Sidekick is a personal macOS notch companion for Codex account switching and Usage. The notch island is the only interface. Add accounts through browser sign-in, switch accounts, and check Usage there. The app manages local authentication snapshots and activates the selected snapshot for Codex.

The intended user flow is:

```text
Open menu
→ inspect weekly Usage and optional 5-hour Usage
→ select account
→ confirm normal switch consequences
→ switch
```

The implementation flow is:

```text
Preflight
→ close Codex Desktop
→ save the current account's latest credentials
→ atomically activate the target credentials
→ verify the target identity through Codex
→ write activeAccountID
→ reopen Codex Desktop
```

The sequence is deliberately linear. Every step either succeeds or returns an error. Verification and registry-commit failures after activation restore the validated original profile credential; the implementation does not add a general recovery state machine around the switch.
