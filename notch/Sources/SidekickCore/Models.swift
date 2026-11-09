// SPDX-License-Identifier: MIT
import Foundation

// Shared account and usage types for the Relay notch UI.

public struct WeeklyUsage: Codable, Equatable, Sendable {
    public let remainingPercent: Int
    public let resetsAt: Date
    public let fiveHourRemainingPercent: Int?
    public let fiveHourResetsAt: Date?

    public init(
        remainingPercent: Int,
        resetsAt: Date,
        fiveHourRemainingPercent: Int? = nil,
        fiveHourResetsAt: Date? = nil
    ) {
        self.remainingPercent = remainingPercent
        self.resetsAt = resetsAt
        self.fiveHourRemainingPercent = fiveHourRemainingPercent
        self.fiveHourResetsAt = fiveHourResetsAt
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
