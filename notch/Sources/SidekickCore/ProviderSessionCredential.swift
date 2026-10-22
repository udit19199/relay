// SPDX-License-Identifier: MIT
import Foundation

/// Browser or API session material for non-Codex profiles (`session.json` in the profile directory).
public struct ProviderSessionCredential: Codable, Equatable, Sendable {
    public var cookieHeader: String?
    public var sessionKey: String?

    public init(cookieHeader: String? = nil, sessionKey: String? = nil) {
        self.cookieHeader = cookieHeader
        self.sessionKey = sessionKey
    }

    public static let fileName = "session.json"

    public func save(to profileHome: URL, fileManager: FileManager = .default) throws {
        let url = profileHome.appending(path: ProviderSessionCredential.fileName)
        let data = try JSONEncoder().encode(self)
        try data.write(to: url, options: .atomic)
    }

    public static func load(from profileHome: URL, fileManager: FileManager = .default) throws -> ProviderSessionCredential {
        let url = profileHome.appending(path: ProviderSessionCredential.fileName)
        guard fileManager.fileExists(atPath: url.path) else {
            throw ProviderUsageError.missingSession
        }
        return try JSONDecoder().decode(ProviderSessionCredential.self, from: Data(contentsOf: url))
    }

    public var normalizedCookieHeader: String? {
        guard let raw = cookieHeader?.trimmingCharacters(in: .whitespacesAndNewlines), !raw.isEmpty else {
            return nil
        }
        if raw.lowercased().hasPrefix("cookie:") {
            let trimmed = raw.dropFirst("cookie:".count).trimmingCharacters(in: .whitespacesAndNewlines)
            return trimmed.isEmpty ? nil : trimmed
        }
        return raw
    }

    public var normalizedSessionKey: String? {
        if let key = sessionKey?.trimmingCharacters(in: .whitespacesAndNewlines), !key.isEmpty {
            return key
        }
        guard let header = normalizedCookieHeader else { return nil }
        for component in header.split(separator: ";") {
            let pair = component.split(separator: "=", maxSplits: 1, omittingEmptySubsequences: false)
            guard pair.count == 2 else { continue }
            let name = pair[0].trimmingCharacters(in: .whitespacesAndNewlines)
            guard name == "sessionKey" else { continue }
            let value = pair[1].trimmingCharacters(in: .whitespacesAndNewlines)
            return value.isEmpty ? nil : value
        }
        return nil
    }

    public var cursorCookieHeader: String? { normalizedCookieHeader }

    public static func parsePasted(_ raw: String, kind: AccountKind) -> ProviderSessionCredential? {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        switch kind {
        case .codex:
            return nil
        case .cursor:
            return ProviderSessionCredential(cookieHeader: trimmed)
        case .claude:
            if trimmed.hasPrefix("sk-ant-") {
                return ProviderSessionCredential(sessionKey: trimmed)
            }
            return ProviderSessionCredential(cookieHeader: trimmed)
        }
    }

    public static func fromCookies(_ cookies: [HTTPCookie], kind: AccountKind) -> ProviderSessionCredential? {
        switch kind {
        case .codex:
            return nil
        case .cursor:
            let relevant = cookies.filter {
                let domain = $0.domain.lowercased()
                return domain.contains("cursor")
            }
            guard !relevant.isEmpty else { return nil }
            let header = relevant.map { "\($0.name)=\($0.value)" }.joined(separator: "; ")
            return ProviderSessionCredential(cookieHeader: header)
        case .claude:
            guard let session = cookies.first(where: { $0.name == "sessionKey" }) else { return nil }
            let value = session.value.trimmingCharacters(in: .whitespacesAndNewlines)
            guard value.hasPrefix("sk-ant-") else { return nil }
            return ProviderSessionCredential(sessionKey: value)
        }
    }
}

public enum ProviderUsageError: LocalizedError, Equatable, Sendable {
    case missingSession
    case unauthorized
    case malformedResponse
    case usageUnavailable

    public var errorDescription: String? {
        switch self {
        case .missingSession:
            "This account has no saved session. Add a session.json file in the profile folder."
        case .unauthorized:
            "The saved session is no longer valid. Update session.json with a fresh login."
        case .malformedResponse:
            "The provider returned data Sidekick could not parse."
        case .usageUnavailable:
            "No usage window is available for this account."
        }
    }
}
