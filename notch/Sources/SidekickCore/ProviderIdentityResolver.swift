// SPDX-License-Identifier: MIT
import Foundation

public struct ResolvedProviderIdentity: Equatable, Sendable {
    public let email: String?
    public let accountID: String?
    public let suggestedDisplayName: String

    public init(email: String?, accountID: String?) {
        self.email = email
        self.accountID = accountID
        if let email, let local = email.split(separator: "@").first {
            suggestedDisplayName = String(local)
        } else {
            suggestedDisplayName = "Account"
        }
    }
}

public enum ProviderIdentityResolver {
    public static func resolve(kind: AccountKind, credential: ProviderSessionCredential) async throws -> ResolvedProviderIdentity {
        switch kind {
        case .codex:
            throw ProviderUsageError.malformedResponse
        case .cursor:
            return try await resolveCursor(credential: credential)
        case .claude:
            return try await resolveClaude(credential: credential)
        }
    }

    private static func resolveCursor(credential: ProviderSessionCredential) async throws -> ResolvedProviderIdentity {
        guard let cookieHeader = credential.cursorCookieHeader else {
            throw ProviderUsageError.missingSession
        }
        let url = URL(string: "https://cursor.com/api/auth/me")!
        var request = URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData)
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.setValue(cookieHeader, forHTTPHeaderField: "Cookie")
        request.timeoutInterval = 20
        let (data, response) = try await URLSession.shared.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw ProviderUsageError.malformedResponse }
        if http.statusCode == 401 || http.statusCode == 403 { throw ProviderUsageError.unauthorized }
        guard http.statusCode == 200 else { throw ProviderUsageError.malformedResponse }
        let json = try JSONSerialization.jsonObject(with: data) as? [String: Any]
        let email = json?["email"] as? String
        let accountID = json?["sub"] as? String
        return ResolvedProviderIdentity(email: email, accountID: accountID)
    }

    private static func resolveClaude(credential: ProviderSessionCredential) async throws -> ResolvedProviderIdentity {
        guard let sessionKey = credential.normalizedSessionKey else {
            throw ProviderUsageError.missingSession
        }
        let url = URL(string: "https://claude.ai/api/organizations")!
        var request = URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData)
        request.setValue("sessionKey=\(sessionKey)", forHTTPHeaderField: "Cookie")
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.timeoutInterval = 20
        let (data, response) = try await URLSession.shared.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw ProviderUsageError.malformedResponse }
        if http.statusCode == 401 || http.statusCode == 403 { throw ProviderUsageError.unauthorized }
        guard http.statusCode == 200 else { throw ProviderUsageError.malformedResponse }
        _ = try JSONSerialization.jsonObject(with: data)
        return ResolvedProviderIdentity(email: nil, accountID: nil)
    }
}
