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
        let response: Response = try await get(path: "/v0/management/auth-files")
        return (response.files ?? []).sorted {
            $0.displayName.localizedCaseInsensitiveCompare($1.displayName) == .orderedAscending
        }
    }

    public func startAddAccount(provider: RelayProvider) async throws -> (url: URL, state: String) {
        struct Response: Decodable { var url: String?; var state: String?; var error: String? }
        let path = provider == .codex
            ? "/v0/management/codex-auth-url"
            : "/v0/management/anthropic-auth-url"
        let response: Response = try await get(path: path)
        if let error = response.error, !error.isEmpty { throw RelayError.serverMessage(error) }
        guard let urlString = response.url, let url = URL(string: urlString),
              let state = response.state, !state.isEmpty
        else { throw RelayError.malformedResponse }
        return (url, state)
    }

    public func pollAddAccount(state: String) async throws -> AddAccountStatus {
        struct Response: Decodable {
            var status: String?
            var error: String?
        }
        var components = URLComponents(url: baseURL.appendingPathComponent("/v0/management/get-auth-status"), resolvingAgainstBaseURL: false)!
        components.queryItems = [URLQueryItem(name: "state", value: state)]
        let response: Response = try await get(url: components.url!)
        switch response.status {
        case "ok": return .done
        case "wait": return .waiting
        default:
            if let error = response.error, !error.isEmpty { throw RelayError.serverMessage(error) }
            throw RelayError.malformedResponse
        }
    }

    public func cancelAddAccount(state: String) async {
        var components = URLComponents(url: baseURL.appendingPathComponent("/v0/management/oauth-session"), resolvingAgainstBaseURL: false)!
        components.queryItems = [URLQueryItem(name: "state", value: state)]
        var request = URLRequest(url: components.url!)
        request.httpMethod = "DELETE"
        authorize(&request)
        _ = try? await session.data(for: request)
    }

    public func removeAccount(name: String) async throws {
        struct Body: Encodable { var name: String }
        struct Response: Decodable { var status: String?; var error: String? }
        let response: Response = try await delete(path: "/v0/management/auth-files", body: Body(name: name))
        if response.status != "ok" {
            throw RelayError.serverMessage(response.error ?? "remove failed")
        }
    }

    // MARK: - HTTP

    private func get<T: Decodable>(path: String) async throws -> T {
        try await get(url: baseURL.appendingPathComponent(path))
    }

    private func get<T: Decodable>(url: URL) async throws -> T {
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        authorize(&request)
        return try await send(request)
    }

    private func delete<B: Encodable, T: Decodable>(path: String, body: B) async throws -> T {
        var request = URLRequest(url: baseURL.appendingPathComponent(path))
        request.httpMethod = "DELETE"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(body)
        authorize(&request)
        return try await send(request)
    }

    private func authorize(_ request: inout URLRequest) {
        request.setValue("Bearer \(managementKey)", forHTTPHeaderField: "Authorization")
    }

    private func send<T: Decodable>(_ request: URLRequest) async throws -> T {
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
