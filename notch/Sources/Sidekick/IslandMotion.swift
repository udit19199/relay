// SPDX-License-Identifier: MIT
import AppKit
import QuartzCore

/// Shared timing for notch open, close, and in-place layout morphs.
enum IslandMotion {
    static let expandDuration: TimeInterval = 0.34
    static let collapseDuration: TimeInterval = 0.28
    static let morphDuration: TimeInterval = 0.30

    static func expandTiming() -> CAMediaTimingFunction {
        CAMediaTimingFunction(controlPoints: 0.22, 1, 0.36, 1)
    }

    /// Ease-in close: accelerates into the compact notch (NotchNook / Alcove style).
    static func collapseTiming() -> CAMediaTimingFunction {
        CAMediaTimingFunction(controlPoints: 0.32, 0, 0.67, 0)
    }

    static func morphTiming() -> CAMediaTimingFunction {
        CAMediaTimingFunction(name: .easeInEaseOut)
    }

    @MainActor
    static var prefersReducedMotion: Bool {
        NSWorkspace.shared.accessibilityDisplayShouldReduceMotion
    }
}
