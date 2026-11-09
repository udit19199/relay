# Product decisions

Sidekick is Relay's face. Every control must directly support one of
three jobs:

1. see per-account 5-hour and weekly usage;
2. add an account through Relay's browser sign-in;
3. remove a credential (or quit).

Relay serves all accounts at once, so the island never switches,
activates, or highlights one account. The compact island shows the
lowest weekly remaining across accounts — the number that decides
whether work continues — plus the lowest 5-hour value when Relay
reports one.

Adding an account opens Relay's OAuth URL in the default browser.
Sidekick polls Relay until the credential lands, then refreshes the
list. There is no paste-a-cookie step and no local sign-in state.

Removing deletes the credential from Relay (`auth-files?name=` is
not used; the JSON-body delete is). The dialog says devices using it
lose access, because unlike the old file-swap switcher this affects
every client through the proxy immediately.

Disabled or cooled-down accounts stay visible with Relay's status
message, so a 429 failover or a tired credential is explainable at a
glance instead of silently missing.
