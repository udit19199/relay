// SPDX-License-Identifier: MIT
import Foundation

/// Drives expanded notch size from the island body content.
enum IslandExpandedBodyLayout: Equatable {
    case accounts(count: Int)
    case addContent(AddAccountLayoutPhase)

    enum AddAccountLayoutPhase: Equatable {
        case chooseProvider
        case pasteSession
    }

    var bodyHeight: CGFloat {
        switch self {
        case let .accounts(count):
            if count == 0 { return 56 }
            return CGFloat(count) * 104 + 16
        case .addContent(.chooseProvider):
            return 360
        case .addContent(.pasteSession):
            return 400
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
