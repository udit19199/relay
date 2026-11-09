// SPDX-License-Identifier: MIT
import Foundation
import Testing

@testable import Sidekick

struct IslandHoverStateTests {
    @Test(arguments: [
        CGRect(x: 600, y: 968, width: 280, height: 32),
        CGRect(x: -1200, y: 1060, width: 480, height: 440),
    ])
    func topEdgeOpensAndKeepsTheIslandOpen(frame: CGRect) {
        let point = CGPoint(x: frame.midX, y: frame.maxY)
        let inside = IslandHoverState.contains(point, in: frame)
        #expect(inside)
        var hover = IslandHoverState()
        #expect(hover.update(inside: inside, expanded: false, keepOpen: false, now: 0) == nil)
        #expect(hover.update(inside: inside, expanded: false, keepOpen: false, now: 0.20) == true)
        #expect(hover.update(inside: inside, expanded: true, keepOpen: false, now: 1) == nil)
        #expect(!IslandHoverState.contains(CGPoint(x: frame.minX - 1, y: frame.maxY), in: frame))
        #expect(!IslandHoverState.contains(CGPoint(x: frame.maxX + 1, y: frame.maxY), in: frame))
        #expect(!IslandHoverState.contains(CGPoint(x: frame.midX, y: frame.maxY + 1), in: frame))
        #expect(!IslandHoverState.contains(CGPoint(x: frame.midX, y: frame.minY - 1), in: frame))
    }

    @Test func stationaryHoverOpensWithoutAnEntryEventOrRestartingTheDelay() {
        var hover = IslandHoverState()
        #expect(hover.update(inside: true, expanded: false, keepOpen: false, now: 0) == nil)
        #expect(hover.update(inside: true, expanded: false, keepOpen: false, now: 0.10) == nil)
        #expect(hover.update(inside: true, expanded: false, keepOpen: false, now: 0.15) == true)
    }

    @Test func briefVisitsDoNotOpenAndReturningCancelsClosing() {
        var hover = IslandHoverState()
        #expect(hover.update(inside: true, expanded: false, keepOpen: false, now: 0) == nil)
        #expect(hover.update(inside: false, expanded: false, keepOpen: false, now: 0.10) == nil)
        #expect(hover.update(inside: false, expanded: false, keepOpen: false, now: 0.30) == nil)
        #expect(hover.update(inside: true, expanded: true, keepOpen: false, now: 1) == nil)
        #expect(hover.update(inside: false, expanded: true, keepOpen: false, now: 2) == nil)
        #expect(hover.update(inside: true, expanded: true, keepOpen: false, now: 2.20) == nil)
        #expect(hover.update(inside: true, expanded: true, keepOpen: false, now: 2.40) == nil)
        #expect(hover.update(inside: false, expanded: true, keepOpen: false, now: 3) == nil)
        #expect(hover.update(inside: false, expanded: true, keepOpen: false, now: 3.30) == false)
    }

    @Test func dismissalRequiresLeavingBeforeHoverCanOpenAgain() {
        var hover = IslandHoverState()
        #expect(hover.update(inside: true, expanded: true, keepOpen: false, now: 0) == nil)
        hover.dismiss()
        #expect(hover.update(inside: true, expanded: false, keepOpen: false, now: 1) == nil)
        #expect(hover.update(inside: false, expanded: false, keepOpen: false, now: 2) == nil)
        #expect(hover.update(inside: true, expanded: false, keepOpen: false, now: 3) == nil)
        #expect(hover.update(inside: true, expanded: false, keepOpen: false, now: 3.20) == true)
    }

    @Test func confirmationKeepsTheIslandOpenUntilTheDialogEnds() {
        var hover = IslandHoverState()
        #expect(hover.update(inside: false, expanded: true, keepOpen: true, now: 0) == nil)
        #expect(hover.update(inside: false, expanded: true, keepOpen: true, now: 1) == nil)
        #expect(hover.update(inside: false, expanded: true, keepOpen: false, now: 1.10) == nil)
        #expect(hover.update(inside: false, expanded: true, keepOpen: false, now: 1.30) == false)
    }
}
