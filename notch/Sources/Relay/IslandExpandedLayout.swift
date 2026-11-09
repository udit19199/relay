// SPDX-License-Identifier: MIT
import Foundation

/// Drives expanded notch size from the island body content.
enum IslandExpandedBodyLayout: Equatable {
    /// Measured height of the error banner, account list and footer.
    case accounts(contentHeight: CGFloat)
    case addContent(AddAccountLayoutPhase)

    enum AddAccountLayoutPhase: Equatable {
        case chooseProvider
        case waitingForBrowser
    }

    private static let footerHeight: CGFloat = 63

    var bodyHeight: CGFloat {
        switch self {
        case let .accounts(contentHeight):
            return max(contentHeight, 56)
        case .addContent(.chooseProvider):
            return 300 + Self.footerHeight
        case .addContent(.waitingForBrowser):
            return 220 + Self.footerHeight
        }
    }

    func panelWidth(expandedWidth: CGFloat) -> CGFloat {
        switch self {
        case .accounts:
            return expandedWidth
        case .addContent:
            return min(expandedWidth, 340)
        }
    }
}
