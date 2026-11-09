// SPDX-License-Identifier: MIT
import Foundation

// Shared account and usage types for the Relay notch UI.

public struct WeeklyUsage: Codable, Equatable, Sendable {
    public let remainingPercent: Int
    public let resetsAt: Date
    public let fiveHourRemainingPercent: Int?
    public let fiveHourResetsAt: Date?
    /// OpenCode Go only.
    public let monthlyRemainingPercent: Int?
    public let monthlyResetsAt: Date?

    public init(
        remainingPercent: Int,
        resetsAt: Date,
        fiveHourRemainingPercent: Int? = nil,
        fiveHourResetsAt: Date? = nil,
        monthlyRemainingPercent: Int? = nil,
        monthlyResetsAt: Date? = nil
    ) {
        self.remainingPercent = remainingPercent
        self.resetsAt = resetsAt
        self.fiveHourRemainingPercent = fiveHourRemainingPercent
        self.fiveHourResetsAt = fiveHourResetsAt
        self.monthlyRemainingPercent = monthlyRemainingPercent
        self.monthlyResetsAt = monthlyResetsAt
    }
}

public enum UsageViewState: Equatable, Sendable {
    case idle
    case loaded(WeeklyUsage)
    case stale(WeeklyUsage, String)
    case unavailable(String)

    public var displayedUsage: WeeklyUsage? {
        switch self {
        case let .loaded(usage), let .stale(usage, _):
            usage
        case .idle, .unavailable:
            nil
        }
    }

    public var refreshError: String? {
        guard case let .stale(_, message) = self else { return nil }
        return message
    }
}

public struct OperationError: LocalizedError, Equatable, Sendable {
    public let title: String
    public let message: String
    public let underlyingDescription: String?

    public init(title: String, message: String, underlyingDescription: String?) {
        self.title = title
        self.message = message
        self.underlyingDescription = underlyingDescription
    }

    public var errorDescription: String? {
        "\(title): \(message)"
    }
}

// MARK: - Pooled usage

extension WeeklyUsage {
    /// Usage as of `now`: a window whose reset time has passed is full again,
    /// even before Relay sees the account's next response headers.
    public func current(at now: Date) -> WeeklyUsage {
        let fiveHourReset = fiveHourResetsAt.map { $0 <= now } ?? false
        let monthlyReset = monthlyResetsAt.map { $0 <= now } ?? false
        return WeeklyUsage(
            remainingPercent: resetsAt <= now ? 100 : remainingPercent,
            resetsAt: resetsAt,
            fiveHourRemainingPercent: fiveHourReset ? 100 : fiveHourRemainingPercent,
            fiveHourResetsAt: fiveHourResetsAt,
            monthlyRemainingPercent: monthlyReset ? 100 : monthlyRemainingPercent,
            monthlyResetsAt: monthlyResetsAt
        )
    }

    /// True while some window is used up and its reset time is still ahead.
    public func isBlocked(at now: Date) -> Bool {
        let usage = current(at: now)
        return usage.remainingPercent == 0 || usage.fiveHourRemainingPercent == 0
            || usage.monthlyRemainingPercent == 0
    }
}

/// One limit window summed over every account. Each account is an equal
/// share, so 100% means every account's window is full.
public struct PooledWindow: Equatable, Sendable {
    public let remainingPercent: Int
    /// Soonest time an account in this window gets capacity back.
    public let nextResetAt: Date?
    /// Pool percentage points that come back at `nextResetAt`.
    public let nextResetGainPercent: Int
}

public struct PooledUsage: Equatable, Sendable {
    public let fiveHour: PooledWindow?
    public let weekly: PooledWindow?

    public init(usages: [WeeklyUsage], now: Date) {
        let current = usages.map { $0.current(at: now) }
        weekly = Self.pool(current.map { ($0.remainingPercent, $0.resetsAt) }, now: now)
        // An account with no weekly left is rejected until its weekly reset,
        // so its 5-hour window adds nothing until then.
        fiveHour = Self.pool(current.compactMap { usage in
            guard let percent = usage.fiveHourRemainingPercent,
                  let resetsAt = usage.fiveHourResetsAt else { return nil }
            if usage.remainingPercent == 0 { return (0, max(resetsAt, usage.resetsAt)) }
            return (percent, resetsAt)
        }, now: now)
    }

    private static func pool(_ windows: [(percent: Int, resetsAt: Date)], now: Date) -> PooledWindow? {
        guard !windows.isEmpty else { return nil }
        let count = Double(windows.count)
        let total = Double(windows.map(\.percent).reduce(0, +)) / count
        let refilling = windows.filter { $0.percent < 100 && $0.resetsAt > now }
        let next = refilling.map(\.resetsAt).min()
        // Resets within a minute of the soonest one count as the same refill.
        let gain = next.map { soonest in
            refilling.filter { $0.resetsAt.timeIntervalSince(soonest) < 60 }
                .map { Double(100 - $0.percent) / count }
                .reduce(0, +)
        } ?? 0
        return PooledWindow(
            remainingPercent: Int(total.rounded()),
            nextResetAt: next,
            nextResetGainPercent: Int(gain.rounded())
        )
    }
}
