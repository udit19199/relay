// SPDX-License-Identifier: MIT
import AppKit
import Foundation
import SidekickCore

enum SystemBrowser {
    static func openSignIn(for kind: AccountKind) {
        NSWorkspace.shared.open(ProviderSignInURLs.signInURL(for: kind))
    }
}
