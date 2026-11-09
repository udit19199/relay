// SPDX-License-Identifier: MIT
import Foundation

public struct ClaudeUsageReader: Sendable {
    public let timeout: Duration
    private let baseURL: URL

    public init(timeout: Duration = .seconds(20), baseURL: URL = URL(string: "https://claude.ai/api")!) {
        self.timeout = timeout
        self.baseURL = baseURL
    }

    public func read(profileHome: URL) async throws -> WeeklyUsage {
        let credential = try ProviderSessionCredential.load(from: profileHome)
        guard let sessionKey = credential.normalizedSessionKey else {
            throw ProviderUsageError.missingSession
        }
        let organizationID = try await fetchPrimaryOrganizationID(sessionKey: sessionKey)
        let payload = try await fetchUsageJSON(orgID: organizationID, sessionKey: sessionKey)
        return try Self.weeklyUsage(from: payload)
    }

    static func weeklyUsage(from json: [String: Any]) throws -> WeeklyUsage {
        var fiveHourRemaining: Int?
        var fiveHourResets: Date?
        if let fiveHour = json["five_hour"] as? [String: Any] {
            if let used = percentUsed(from: fiveHour["utilization"]) {
                fiveHourRemaining = remainingPercent(fromUsed: used)
            }
            if let resets = fiveHour["resets_at"] as? String {
                fiveHourResets = ProviderISO8601.parse(resets)
            }
        }

        guard let sevenDay = json["seven_day"] as? [String: Any],
              let weeklyUsed = percentUsed(from: sevenDay["utilization"]),
              let weeklyResets = (sevenDay["resets_at"] as? String).flatMap(ProviderISO8601.parse)
        else {
            throw ProviderUsageError.usageUnavailable
        }

        return WeeklyUsage(
            remainingPercent: remainingPercent(fromUsed: weeklyUsed),
            resetsAt: weeklyResets,
            fiveHourRemainingPercent: fiveHourRemaining,
            fiveHourResetsAt: fiveHourResets
        )
    }

    private static func percentUsed(from value: Any?) -> Double? {
        if let number = value as? Double { return number }
        if let number = value as? Int { return Double(number) }
        if let string = value as? String { return Double(string) }
        return nil
    }

    private static func remainingPercent(fromUsed used: Double) -> Int {
        max(0, min(100, Int((100 - used).rounded())))
    }

    private func fetchPrimaryOrganizationID(sessionKey: String) async throws -> String {
        let url = baseURL.appending(path: "organizations")
        let data = try await get(url: url, sessionKey: sessionKey)
        guard let organizations = try JSONSerialization.jsonObject(with: data) as? [[String: Any]],
              let first = organizations.first,
              let id = first["uuid"] as? String ?? first["id"] as? String
        else {
            throw ProviderUsageError.malformedResponse
        }
        return id
    }

    private func fetchUsageJSON(orgID: String, sessionKey: String) async throws -> [String: Any] {
        let url = baseURL.appending(path: "organizations/\(orgID)/usage")
        let data = try await get(url: url, sessionKey: sessionKey)
        guard let json = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw ProviderUsageError.malformedResponse
        }
        return json
    }

    private func get(url: URL, sessionKey: String) async throws -> Data {
        var request = URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData)
        request.httpMethod = "GET"
        request.setValue("sessionKey=\(sessionKey)", forHTTPHeaderField: "Cookie")
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.timeoutInterval = Self.timeoutInterval(timeout)
        let (data, response) = try await URLSession.shared.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw ProviderUsageError.malformedResponse }
        if http.statusCode == 401 || http.statusCode == 403 { throw ProviderUsageError.unauthorized }
        guard http.statusCode == 200 else { throw ProviderUsageError.malformedResponse }
        return data
    }

    private static func timeoutInterval(_ timeout: Duration) -> TimeInterval {
        let components = timeout.components
        return Double(components.seconds) + Double(components.attoseconds) / 1e18
    }
}
