// SPDX-License-Identifier: MIT
import Foundation

// One Relay credential, as listed by the local proxy's management API.
// Relay serves all of these concurrently; there is no "active" account.
public struct RelayAccount: Codable, Identifiable, Equatable, Hashable, Sendable {
    public let authIndex: String
    public let name: String
    public let provider: String
    public let email: String?
    public let displayName: String
    public let disabled: Bool
    public let unavailable: Bool
    public let statusMessage: String?
    public let quotaSignals: [String: String]

    public var id: String { name }

    public init(
        authIndex: String,
        name: String,
        provider: String,
        email: String? = nil,
        displayName: String? = nil,
        disabled: Bool = false,
        unavailable: Bool = false,
        statusMessage: String? = nil,
        quotaSignals: [String: String] = [:]
    ) {
        self.authIndex = authIndex
        self.name = name
        self.provider = provider
        self.email = email
        self.displayName = displayName ?? email ?? name
        self.disabled = disabled
        self.unavailable = unavailable
        self.statusMessage = statusMessage
        self.quotaSignals = quotaSignals
    }

    private enum CodingKeys: String, CodingKey {
        case authIndex = "auth_index"
        case name
        case typeName = "type"
        case provider
        case label
        case email
        case disabled
        case unavailable
        case statusMessage = "status_message"
        case quota
    }

    private struct QuotaPayload: Decodable {
        var signals: [String: LosslessStringValue]?

        private enum CodingKeys: String, CodingKey { case signals }

        init(from decoder: any Decoder) throws {
            let container = try decoder.container(keyedBy: CodingKeys.self)
            signals = try container.decodeIfPresent([String: LosslessStringValue].self, forKey: .signals)
        }
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        let index = try container.decodeIfPresent(String.self, forKey: .authIndex) ?? ""
        let name = try container.decodeIfPresent(String.self, forKey: .name) ?? index
        let provider = try container.decodeIfPresent(String.self, forKey: .provider)
            ?? (try container.decodeIfPresent(String.self, forKey: .typeName) ?? "")
        let rawEmail = try container.decodeIfPresent(String.self, forKey: .email)
        let label = try container.decodeIfPresent(String.self, forKey: .label)
        let disabled = try container.decodeIfPresent(Bool.self, forKey: .disabled) ?? false
        let unavailable = try container.decodeIfPresent(Bool.self, forKey: .unavailable) ?? false
        let statusMessage = try container.decodeIfPresent(String.self, forKey: .statusMessage)
        let quota = try container.decodeIfPresent(QuotaPayload.self, forKey: .quota)
        var signals: [String: String] = [:]
        for (key, value) in quota?.signals ?? [:] { signals[key] = value.value }
        self.init(
            authIndex: index,
            name: name.isEmpty ? index : name,
            provider: provider,
            email: rawEmail.flatMap { $0.isEmpty ? nil : $0 },
            displayName: label.flatMap { $0.isEmpty ? nil : $0 },
            disabled: disabled,
            unavailable: unavailable,
            statusMessage: statusMessage,
            quotaSignals: signals
        )
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(authIndex, forKey: .authIndex)
        try container.encode(name, forKey: .name)
        try container.encode(provider, forKey: .provider)
        try container.encodeIfPresent(email, forKey: .email)
        try container.encodeIfPresent(statusMessage, forKey: .statusMessage)
    }
}

// Decodes a JSON string or number into its string form. Relay's quota
// signal values arrive as strings today; this keeps working if they
// ever arrive as raw numbers.
struct LosslessStringValue: Decodable {
    let value: String

    init(from decoder: any Decoder) throws {
        let container = try decoder.singleValueContainer()
        if let string = try? container.decode(String.self) {
            value = string
        } else if let double = try? container.decode(Double.self) {
            value = String(double)
        } else if let int = try? container.decode(Int.self) {
            value = String(int)
        } else if let bool = try? container.decode(Bool.self) {
            value = bool ? "true" : "false"
        } else {
            value = ""
        }
    }
}

// MARK: - Status text

extension RelayAccount {
    // Relay passes the upstream error body through as the status message,
    // often raw JSON. Turn it into one short line for the island.
    public var statusSummary: String {
        if disabled { return "Disabled in Relay" }
        guard let message = statusMessage?.trimmingCharacters(in: .whitespacesAndNewlines),
              !message.isEmpty else { return "Unavailable" }
        struct Body: Decodable {
            struct Detail: Decodable { var type: String?; var message: String? }
            var error: Detail?
        }
        if let data = message.data(using: .utf8),
           let detail = (try? JSONDecoder().decode(Body.self, from: data))?.error {
            if detail.type == "rate_limit_error" { return "Rate limited until the limit resets" }
            if let text = detail.message, !text.isEmpty { return text }
        }
        return message
    }
}

// MARK: - Usage from entry signals

extension RelayAccount {
    // Maps Relay's observed quota signals to display usage. Relay's panel
    // data: "<scope>-<window>-Utilization" (0..1 fraction used) paired with
    // "<scope>-<window>-Reset" (epoch seconds). Weekly is required; the
    // 5-hour window is optional.
    public func relayUsage() -> WeeklyUsage? {
        guard let weekly = windowUsage(containing: ["7d", "week", "weekly"]) else { return nil }
        let fiveHour = windowUsage(containing: ["5h", "5hour", "300min"])
        return WeeklyUsage(
            remainingPercent: weekly.remainingPercent,
            resetsAt: weekly.resetsAt,
            fiveHourRemainingPercent: fiveHour?.remainingPercent,
            fiveHourResetsAt: fiveHour?.resetsAt
        )
    }

    private func windowUsage(containing markers: [String]) -> (remainingPercent: Int, resetsAt: Date)? {
        let lowered = Dictionary(uniqueKeysWithValues: quotaSignals.map { ($0.key.lowercased(), $0.value) })
        guard let utilKey = lowered.keys.first(where: { key in
            markers.contains(where: { key.contains($0) }) && key.contains("util")
        }) else { return nil }
        let resetKey = utilKey.replacingOccurrences(of: "utilization", with: "reset")
            .replacingOccurrences(of: "utilisation", with: "reset")
        guard let used = Double(lowered[utilKey] ?? ""),
              let resetValue = lowered[resetKey],
              let resetsAt = RelayAccount.parseReset(resetValue)
        else { return nil }
        let remaining = min(100, max(0, Int(((1 - used) * 100).rounded())))
        return (remaining, resetsAt)
    }

    private static func parseReset(_ value: String) -> Date? {
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        if let epoch = Double(trimmed), epoch > 1_000_000_000 {
            return Date(timeIntervalSince1970: epoch)
        }
        return RelayResetTime.parse(trimmed)
    }
}

public enum RelayProvider: String, CaseIterable, Sendable {
    case claude
    case codex

    public var displayName: String {
        switch self {
        case .claude: "Claude"
        case .codex: "Codex"
        }
    }

    public var systemImage: String {
        switch self {
        case .claude: "bubble.left.and.bubble.right"
        case .codex: "terminal"
        }
    }
}
