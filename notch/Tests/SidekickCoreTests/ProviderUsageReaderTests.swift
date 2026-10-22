// SPDX-License-Identifier: MIT
import Foundation
import Testing
@testable import SidekickCore

struct ProviderUsageReaderTests {
    @Test func cursorMapsPlanPercentToWeeklyRemaining() throws {
        let data = Data(
            """
            {
              "billingCycleEnd": "2026-02-01T00:00:00Z",
              "individualUsage": {
                "plan": { "totalPercentUsed": 36 }
              }
            }
            """.utf8
        )
        let summary = try JSONDecoder().decode(CursorUsageSummary.self, from: data)
        let usage = try CursorUsageReader.weeklyUsage(from: summary)
        #expect(usage.remainingPercent == 64)
        #expect(usage.fiveHourRemainingPercent == nil)
        #expect(usage.resetsAt == ProviderISO8601.parse("2026-02-01T00:00:00Z"))
    }

    @Test func claudeMapsSessionAndWeeklyWindows() throws {
        let json: [String: Any] = [
            "five_hour": [
                "utilization": 20,
                "resets_at": "2026-02-01T05:00:00Z",
            ],
            "seven_day": [
                "utilization": 40,
                "resets_at": "2026-02-08T00:00:00Z",
            ],
        ]
        let usage = try ClaudeUsageReader.weeklyUsage(from: json)
        #expect(usage.fiveHourRemainingPercent == 80)
        #expect(usage.remainingPercent == 60)
    }

    @Test func pastedCursorCookieIsAccepted() {
        let credential = ProviderSessionCredential.parsePasted(
            "WorkosCursorSessionToken=abc",
            kind: .cursor
        )
        #expect(credential?.normalizedCookieHeader == "WorkosCursorSessionToken=abc")
    }

    @Test func cursorCookieHeaderBuildsFromWebKitCookies() {
        let cookies = [
            HTTPCookie(properties: [
                .domain: ".cursor.com",
                .path: "/",
                .name: "WorkosCursorSessionToken",
                .value: "user_01::token",
                .secure: "TRUE",
            ])!,
        ]
        let credential = ProviderSessionCredential.fromCookies(cookies, kind: .cursor)
        #expect(credential?.normalizedCookieHeader?.contains("WorkosCursorSessionToken=") == true)
    }

    @Test func accountProfileDefaultsKindToCodex() throws {
        let id = UUID()
        let created = Date(timeIntervalSinceReferenceDate: 0)
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        let original = AccountProfile(
            id: id,
            kind: .codex,
            displayName: "legacy",
            email: "a@b.com",
            accountID: "acct",
            createdAt: created
        )
        let encoded = try encoder.encode(original)
        var object = try JSONSerialization.jsonObject(with: encoded) as! [String: Any]
        object.removeValue(forKey: "kind")
        let legacy = try JSONSerialization.data(withJSONObject: object)
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let profile = try decoder.decode(AccountProfile.self, from: legacy)
        #expect(profile.kind == .codex)
    }
}
