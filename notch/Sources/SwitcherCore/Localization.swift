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
            "settings_general": "General",
            "usage": "Usage",
            "five_hour": "5h",
            "weekly": "7d",
            "usage_unavailable": "Usage unavailable",
            "left": "% left",
            "resets": "Resets",
            "manage": "Manage Accounts",
            "settings": "Settings",
            "quit": "Quit",
            "switch_title": "Switch to %@?",
            "switch_body": "Codex Desktop will close and reopen. Finish or stop running Desktop tasks first. If Desktop asks to quit, complete its quit dialog. Switching stops if Desktop cannot exit normally. Existing CLI sessions stay open; new CLI sessions use the selected account.",
            "cancel": "Cancel",
            "switch": "Switch Account",
            "accounts": "Accounts",
            "back": "Back",
            "add_account": "Add Account",
            "cancel_add_account": "Cancel Adding Account",
            "remove": "Remove",
            "active": "Active",
            "remove_title": "Remove %@?",
            "remove_body": "This removes only the saved local profile from this Mac.",
            "launch_at_login": "Launch at Login",
            "launch_at_login_requires_approval": "Approval is required in System Settings.",
            "launch_at_login_unavailable": "macOS could not find this Login Item.",
            "open_system_settings": "Open System Settings",
            "show_menu_bar_percentage": "Show Percentage in Menu Bar",
            "show_five_hour_usage": "Show 5-hour Usage",
            "sign_in_hint": "A browser window will open for Codex sign-in.",
            "sign_in_pending_hint": "Complete sign-in in the browser, or cancel here if you closed it.",
            "no_accounts": "No saved accounts",
            "ok": "OK",
            "operation_failed": "Operation failed",
            "switch_failed": "Account switch failed",
            "register_current_account": "Register Current Account",
            "active_unconfirmed": "Active account could not be confirmed",
            "switched_reopen_title": "Account switched",
            "switched_reopen_message": "The selected account is active, but Codex Desktop could not be reopened. Open Codex manually to continue.",
    ]
}
