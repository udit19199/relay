// SPDX-License-Identifier: MIT
import Foundation

public struct CursorUsageReader: Sendable {
    public let timeout: Duration
    private let baseURL: URL

    public init(timeout: Duration = .seconds(20), baseURL: URL = URL(string: "https://cursor.com")!) {
        self.timeout = timeout
        self.baseURL = baseURL
    }

    public func read(profileHome: URL) async throws -> WeeklyUsage {
        let credential = try ProviderSessionCredential.load(from: profileHome)
        guard let cookieHeader = credential.cursorCookieHeader else {
            throw ProviderUsageError.missingSession
        }
        let summary = try await fetchUsageSummary(cookieHeader: cookieHeader)
        return try Self.weeklyUsage(from: summary)
    }

    static func weeklyUsage(from summary: CursorUsageSummary) throws -> WeeklyUsage {
        let usedPercent = planPercentUsed(summary)
        guard usedPercent.isFinite else { throw ProviderUsageError.usageUnavailable }
        let remaining = Self.remainingPercent(fromUsed: usedPercent)
        guard let resetsAt = ProviderISO8601.parse(summary.billingCycleEnd) else {
            throw ProviderUsageError.usageUnavailable
        }
        return WeeklyUsage(remainingPercent: remaining, resetsAt: resetsAt)
    }

    private static func planPercentUsed(_ summary: CursorUsageSummary) -> Double {
        let plan = summary.individualUsage?.plan
        if let total = plan?.totalPercentUsed { return total }
        if let api = plan?.apiPercentUsed, let auto = plan?.autoPercentUsed {
            return (api + auto) / 2
        }
        if let api = plan?.apiPercentUsed { return api }
        if let auto = plan?.autoPercentUsed { return auto }
        let used = Double(plan?.used ?? 0)
        let limit = Double(plan?.limit ?? 0)
        if limit > 0 { return used / limit * 100 }
        let overallUsed = Double(summary.individualUsage?.overall?.used ?? 0)
        let overallLimit = Double(summary.individualUsage?.overall?.limit ?? 0)
        if overallLimit > 0 { return overallUsed / overallLimit * 100 }
        return 0
    }

    private static func remainingPercent(fromUsed used: Double) -> Int {
        max(0, min(100, Int((100 - used).rounded())))
    }

    private func fetchUsageSummary(cookieHeader: String) async throws -> CursorUsageSummary {
        let url = baseURL.appending(path: "api/usage-summary")
        var request = URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData)
        request.httpMethod = "GET"
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.setValue(cookieHeader, forHTTPHeaderField: "Cookie")
        request.timeoutInterval = Self.timeoutInterval(timeout)
        let (data, response) = try await URLSession.shared.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw ProviderUsageError.malformedResponse }
        if http.statusCode == 401 || http.statusCode == 403 { throw ProviderUsageError.unauthorized }
        guard http.statusCode == 200 else { throw ProviderUsageError.malformedResponse }
        do {
            return try JSONDecoder().decode(CursorUsageSummary.self, from: data)
        } catch {
            throw ProviderUsageError.malformedResponse
        }
    }

    private static func timeoutInterval(_ timeout: Duration) -> TimeInterval {
        let components = timeout.components
        return Double(components.seconds) + Double(components.attoseconds) / 1e18
    }
}

// MARK: - Cursor API models (subset of CodexBar)

struct CursorUsageSummary: Decodable, Sendable {
    let billingCycleStart: String?
    let billingCycleEnd: String?
    let individualUsage: CursorIndividualUsage?
}

struct CursorIndividualUsage: Decodable, Sendable {
    let plan: CursorPlanUsage?
    let overall: CursorOverallUsage?
}

struct CursorPlanUsage: Decodable, Sendable {
    let used: Int?
    let limit: Int?
    let autoPercentUsed: Double?
    let apiPercentUsed: Double?
    let totalPercentUsed: Double?
}

struct CursorOverallUsage: Decodable, Sendable {
    let used: Int?
    let limit: Int?
}

enum ProviderISO8601 {
    static func parse(_ value: String?) -> Date? {
        guard let value, !value.isEmpty else { return nil }
        let fractional = ISO8601DateFormatter()
        fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = fractional.date(from: value) { return date }
        let plain = ISO8601DateFormatter()
        plain.formatOptions = [.withInternetDateTime]
        return plain.date(from: value)
    }
}
