import Foundation
import Testing
@testable import Sidekick
@testable import SidekickCore

struct ResetCountdownTests {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func text(_ interval: TimeInterval) -> String {
        ResetCountdown.text(until: now.addingTimeInterval(interval), now: now)
    }

    @Test func justElapsedResetsShowNow() {
        #expect(text(-30) == "Resets now")
    }

    @Test func underAMinuteShowsAMoment() {
        #expect(text(45) == "Resets in a moment")
    }

    @Test func minutesAreCompact() {
        #expect(text(45 * 60) == "Resets in 45m")
    }

    @Test func hoursKeepMinutes() {
        #expect(text(2 * 3_600 + 14 * 60) == "Resets in 2h 14m")
    }

    @Test func wholeDaysDropTheRemainder() {
        #expect(text(2 * 86_400) == "Resets in 2d")
    }

    @Test func daysPlusHoursPrecision() {
        #expect(text(3 * 86_400 + 4 * 3_600) == "Resets in 3d 4h")
    }

    @Test func aWholeHourShowsZeroMinutes() {
        #expect(text(3_600) == "Resets in 1h 0m")
    }

    @Test func hoverTextKeepsTheExactTimestamp() {
        let withDate = ResetCountdown.absoluteText(until: now, includesDate: true)
        let withoutDate = ResetCountdown.absoluteText(until: now, includesDate: false)
        #expect(withDate.hasPrefix("Resets "))
        #expect(withDate.count > withoutDate.count)
    }
}
