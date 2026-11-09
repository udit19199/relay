// SPDX-License-Identifier: MIT
import Foundation

// Hover decisions use current position, independent of AppKit event order.
struct IslandHoverState {
    static func contains(_ point: CGPoint, in frame: CGRect) -> Bool {
        // CGRect.contains excludes maxY, which is the top screen edge in AppKit.
        !frame.isEmpty && point.x >= frame.minX && point.x <= frame.maxX
            && point.y >= frame.minY && point.y <= frame.maxY
    }

    private var wasInside: Bool?
    private var changedAt: Double = 0
    private var dismissed = false

    mutating func dismiss() {
        dismissed = true
    }

    mutating func update(inside: Bool, expanded: Bool, keepOpen: Bool, now: Double) -> Bool? {
        if inside != wasInside {
            wasInside = inside
            changedAt = now
        }
        if !inside { dismissed = false }
        if keepOpen {
            changedAt = now
            return nil
        }
        if inside {
            guard !expanded, !dismissed, now - changedAt >= 0.14 else { return nil }
            return true
        }
        guard expanded, now - changedAt >= 0.26 else { return nil }
        return false
    }
}
