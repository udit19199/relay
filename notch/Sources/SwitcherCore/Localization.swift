import Foundation

// Shared product copy; platform names are adapted by the native presentation layer.

public enum L10n {
    public static func allStrings() -> [String: String] {
        strings
    }
    public static func string(_ key: String) -> String {
        strings[key] ?? key
    }

    private static let strings: [String: String] = [
            "usage": "Usage",
            "five_hour": "5h",
            "weekly": "7d",
            "usage_unavailable": "Usage unavailable",
            "left": "% left",
            "resets": "Resets",
            "quit": "Quit",
            "switch_title": "Switch to %@?",
            "switch_body": "Codex Desktop will close and reopen. Finish or stop running Desktop tasks first. If Desktop asks to quit, complete its quit dialog. Switching stops if Desktop cannot exit normally. Existing CLI sessions stay open; new CLI sessions use the selected account.",
            "cancel": "Cancel",
            "switch": "Switch Account",
            "add_account": "Add Account",
            "cancel_add_account": "Cancel Adding Account",
            "remove": "Remove",
            "remove_title": "Remove %@?",
            "remove_body": "This removes only the saved local profile from this Mac.",
            "sign_in_hint": "A browser window will open for Codex sign-in.",
            "sign_in_pending_hint": "Complete sign-in in the browser, or cancel here if you closed it.",
            "no_accounts": "No saved accounts",
            "ok": "OK",
            "operation_failed": "Operation failed",
            "switch_failed": "Account switch failed",
            "switched_reopen_title": "Account switched",
            "switched_reopen_message": "The selected account is active, but Codex Desktop could not be reopened. Open Codex manually to continue.",
    ]
}
