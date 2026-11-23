// SPDX-License-Identifier: MIT
import Foundation

// Talks to the local Relay proxy's management API (127.0.0.1:8317).
// Same user, same Mac: auth is a Bearer management key read from
// ~/.relay/management.key (0600). Relay's own config holds the same secret.
public struct RelayClient: Sendable {
    public let baseURL: URL
    public let managementKey: String
    public let session: URLSession

    public init(baseURL: URL, managementKey: String, session: URLSession? = nil) {
        self.baseURL = baseURL
        self.managementKey = managementKey
        if let session {
            self.session = session
        } else {
            let config = URLSessionConfiguration.ephemeral
            config.timeoutIntervalForRequest = 20
            config.timeoutIntervalForResource = 20
            self.session = URLSession(configuration: config)
        }
    }

    public static func live() throws -> RelayClient {
        let keyURL = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".relay")
            .appendingPathComponent("management.key")
        let raw = try String(contentsOf: keyURL, encoding: .utf8)
        let key = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !key.isEmpty else { throw RelayError.missingManagementKey }
        return RelayClient(
            baseURL: URL(string: "http://127.0.0.1:8317")!,
            managementKey: key
        )
    }

    // The auth-files entries already carry each credential's observed
    // quota signals, so one call returns accounts and usage together.
    public func listAccounts() async throws -> [RelayAccount] {
        struct Response: Decodable { var files: [RelayAccount]? }
        let response: Response = try await request(method: "GET", path: "/v0/management/auth-files")
        // Claude accounts first (they share the total row), then other providers.
        return (response.files ?? []).sorted {
            let leftClaude = $0.provider == RelayProvider.claude.rawValue
            let rightClaude = $1.provider == RelayProvider.claude.rawValue
            if leftClaude != rightClaude { return leftClaude }
            if $0.provider != $1.provider { return $0.provider < $1.provider }
            return $0.displayName.localizedCaseInsensitiveCompare($1.displayName) == .orderedAscending
        }
    }

    public func startClaudeSignIn() async throws -> (url: URL, state: String) {
        struct Response: Decodable { var url: String?; var state: String?; var error: String? }
        let response: Response = try await request(method: "GET", path: "/v0/management/anthropic-auth-url")
        if let error = response.error, !error.isEmpty { throw RelayError.serverMessage(error) }
        guard let urlString = response.url, let url = URL(string: urlString),
              let state = response.state, !state.isEmpty
        else { throw RelayError.malformedResponse }
        return (url, state)
    }

    public func pollAddAccount(state: String) async throws -> AddAccountStatus {
        let response: StatusResponse = try await request(
            method: "GET", path: "/v0/management/get-auth-status",
            query: [URLQueryItem(name: "state", value: state)]
        )
        switch response.status {
        case "ok": return .done
        case "wait": return .waiting
        default:
            if let error = response.error, !error.isEmpty { throw RelayError.serverMessage(error) }
            throw RelayError.malformedResponse
        }
    }

    public func cancelAddAccount(state: String) async {
        let _: StatusResponse? = try? await request(
            method: "DELETE", path: "/v0/management/oauth-session",
            query: [URLQueryItem(name: "state", value: state)]
        )
    }

    public func removeAccount(name: String) async throws {
        struct Body: Encodable { var name: String }
        let response: StatusResponse = try await request(
            method: "DELETE", path: "/v0/management/auth-files",
            body: JSONEncoder().encode(Body(name: name))
        )
        if response.status != "ok" {
            throw RelayError.serverMessage(response.error ?? "remove failed")
        }
    }

    /// Saves a credential file in Relay's auth directory; Relay loads it on its own.
    public func uploadAuthFile(name: String, json: Data) async throws {
        let response: StatusResponse = try await request(
            method: "POST", path: "/v0/management/auth-files",
            query: [URLQueryItem(name: "name", value: name)], body: json
        )
        if response.status != "ok" { throw RelayError.serverMessage(response.error ?? "Relay did not save the account") }
    }

    // Relay's quota endpoint: OpenCode Go reports rolling, weekly and monthly
    // windows here; Claude reports its 5-hour and weekly windows from the live
    // account usage, not from the last proxied response.
    public func fetchQuota(authIndex: String) async throws -> WeeklyUsage {
        struct Body: Encodable { var auth_index: String }
        struct Bucket: Decodable { var window: String?; var remainingFraction: Double?; var resetTime: String? }
        struct Group: Decodable { var buckets: [Bucket]? }
        struct Response: Decodable { var groups: [Group]? }
        let response: Response = try await request(
            method: "POST", path: "/v0/management/quota/fetch",
            body: JSONEncoder().encode(Body(auth_index: authIndex))
        )
        var windows: [String: (percent: Int, resetsAt: Date)] = [:]
        for bucket in (response.groups ?? []).flatMap({ $0.buckets ?? [] }) {
            guard let name = bucket.window, let fraction = bucket.remainingFraction,
                  let resetsAt = bucket.resetTime.flatMap(RelayResetTime.parse) else { continue }
            windows[name] = (min(100, max(0, Int((fraction * 100).rounded()))), resetsAt)
        }
        guard let weekly = windows["weekly"] else { throw RelayError.malformedResponse }
        let fiveHour = windows["5h"] ?? windows["rolling"]
        return WeeklyUsage(
            remainingPercent: weekly.percent,
            resetsAt: weekly.resetsAt,
            fiveHourRemainingPercent: fiveHour?.percent,
            fiveHourResetsAt: fiveHour?.resetsAt,
            monthlyRemainingPercent: windows["monthly"]?.percent,
            monthlyResetsAt: windows["monthly"]?.resetsAt
        )
    }

    // MARK: - HTTP

    private struct StatusResponse: Decodable { var status: String?; var error: String? }

    private func request<T: Decodable>(
        method: String, path: String, query: [URLQueryItem] = [], body: Data? = nil
    ) async throws -> T {
        var components = URLComponents(url: baseURL.appendingPathComponent(path), resolvingAgainstBaseURL: false)!
        if !query.isEmpty { components.queryItems = query }
        var request = URLRequest(url: components.url!)
        request.httpMethod = method
        request.setValue("Bearer \(managementKey)", forHTTPHeaderField: "Authorization")
        if let body {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = body
        }
        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.data(for: request)
        } catch is URLError {
            throw RelayError.relayUnreachable
        }
        guard let http = response as? HTTPURLResponse else { throw RelayError.malformedResponse }
        switch http.statusCode {
        case 200 ..< 300:
            do {
                return try JSONDecoder().decode(T.self, from: data)
            } catch {
                throw RelayError.malformedResponse
            }
        case 401, 403:
            throw RelayError.unauthorized
        case 422:
            throw RelayError.keyRejected
        default:
            if let message = String(data: data, encoding: .utf8),
               !message.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
               message.count < 300
            {
                throw RelayError.serverMessage(message)
            }
            throw RelayError.requestFailed(http.statusCode)
        }
    }
}

public enum AddAccountStatus: Sendable, Equatable {
    case waiting
    case done
}

public enum RelayError: LocalizedError, Equatable, Sendable {
    case missingManagementKey
    case relayUnreachable
    case unauthorized
    case malformedResponse
    case requestFailed(Int)
    case serverMessage(String)
    case keyRejected

    public var errorDescription: String? {
        switch self {
        case .missingManagementKey:
            "Relay management key is missing. Put it in ~/.relay/management.key (same secret as remote-management.secret-key in ~/.relay/config.yaml)."
        case .relayUnreachable:
            "Relay is not answering at 127.0.0.1:8317. Check the com.relay.proxy LaunchAgent is running."
        case .unauthorized:
            "Relay rejected the management key. Check ~/.relay/management.key matches the secret in ~/.relay/config.yaml."
        case .malformedResponse:
            "Relay returned a response the app did not understand."
        case let .requestFailed(code):
            "Relay request failed (HTTP \(code))."
        case let .serverMessage(message):
            message
        case .keyRejected:
            "The provider rejected this key."
        }
    }
}

enum RelayResetTime {
    static func parse(_ value: String) -> Date? {
        guard !value.isEmpty else { return nil }
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = formatter.date(from: value) { return date }
        formatter.formatOptions = [.withInternetDateTime]
        return formatter.date(from: value)
    }
}
