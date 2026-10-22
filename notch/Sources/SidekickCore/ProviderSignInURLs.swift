// SPDX-License-Identifier: MIT
import Foundation

public enum ProviderSignInURLs {
    public static func signInURL(for kind: AccountKind) -> URL {
        switch kind {
        case .codex:
            URL(string: "https://chatgpt.com")!
        case .cursor:
            URL(string: "https://authenticator.cursor.sh")!
        case .claude:
            URL(string: "https://claude.ai/login")!
        }
    }
}
