import Foundation

// Read-only HTTP requests. Login and reset redemption remain separate operations.
struct DirectUsageReader {
    let timeout: Duration

    func read(profileHome: URL) async throws -> WeeklyUsage {
        let credential: Credential
        do {
            credential = try JSONDecoder().decode(
                Credential.self, from: Data(contentsOf: profileHome.appending(path: "auth.json"))
            )
        } catch {
            throw DirectUsageError.signInRequired
        }
        guard let token = credential.tokens?.access_token, !token.isEmpty,
              let accountID = credential.tokens?.account_id, !accountID.isEmpty else {
            throw DirectUsageError.signInRequired
        }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.httpCookieStorage = nil
        configuration.urlCache = nil
        let session = URLSession(configuration: configuration)
        defer { session.invalidateAndCancel() }
        let usage = try await fetch("usage", token: token, accountID: accountID, session: session)
        let windows = [usage["rate_limit"]?["primary_window"], usage["rate_limit"]?["secondary_window"]]
            .compactMap(Self.window)
        // Failure preserves the account's last complete snapshot, including its reset count.
        let inventory = try await fetch("rate-limit-reset-credits", token: token, accountID: accountID, session: session)
        guard let count = inventory["available_count"]?.intValue, count >= 0,
              case let .array(values)? = inventory["credits"] else {
            throw CodexClientError.malformedResponse
        }
        let credits = values.compactMap { value -> BankedResetCredit? in
            guard value["status"]?.stringValue == "available",
                  let id = value["id"]?.stringValue else { return nil }
            let expiration = value["expires_at"]?.stringValue.flatMap(Self.date)
            return BankedResetCredit(id: id, expiresAt: expiration)
        }.sorted { ($0.expiresAt ?? .distantFuture) < ($1.expiresAt ?? .distantFuture) }
        return try WeeklyUsageNormalizer.normalize(windows, availableResetCount: count, availableResetCredits: credits)
    }

    private func fetch(_ path: String, token: String, accountID: String, session: URLSession) async throws -> JSONValue {
        let url = URL(string: "https://chatgpt.com/backend-api/wham/\(path)")!
        let components = timeout.components
        var request = URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData,
                                 timeoutInterval: Double(components.seconds) + Double(components.attoseconds) / 1e18)
        request.httpMethod = "GET"
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.setValue(accountID, forHTTPHeaderField: "ChatGPT-Account-Id")
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.setValue("Sidekick", forHTTPHeaderField: "User-Agent")
        request.setValue("codex-1", forHTTPHeaderField: "OpenAI-Beta")
        request.setValue("Codex Desktop", forHTTPHeaderField: "originator")
        let (data, response) = try await session.data(for: request)
        guard let response = response as? HTTPURLResponse else { throw CodexClientError.malformedResponse }
        if response.statusCode == 401 || response.statusCode == 403 { throw DirectUsageError.signInRequired }
        guard response.statusCode == 200 else { throw DirectUsageError.httpStatus(response.statusCode) }
        do { return try JSONDecoder().decode(JSONValue.self, from: data) }
        catch { throw CodexClientError.malformedResponse }
    }

    static func window(_ value: JSONValue?) -> RateLimitWindow? {
        guard let value, let used = value["used_percent"]?.doubleValue,
              let seconds = value["limit_window_seconds"]?.intValue, seconds > 0, seconds % 60 == 0,
              let reset = value["reset_at"]?.doubleValue else { return nil }
        return RateLimitWindow(usedPercent: used, windowDurationMins: seconds / 60, resetsAt: reset)
    }

    private static func date(_ value: String) -> Date? {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter.date(from: value) ?? ISO8601DateFormatter().date(from: value)
    }

    private struct Credential: Decodable { let tokens: Tokens? }
    private struct Tokens: Decodable { let access_token: String?; let account_id: String? }
}

enum DirectUsageError: LocalizedError {
    case signInRequired
    case httpStatus(Int)

    var errorDescription: String? {
        switch self {
        case .signInRequired: "Usage could not authenticate. Sign in to this account again."
        case let .httpStatus(status): "Usage request failed (HTTP \(status))."
        }
    }
}
