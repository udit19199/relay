// SPDX-License-Identifier: MIT
import Foundation

public struct ProfileUsageService: Sendable {
    private let codex: any AccountClient
    private let cursor: CursorUsageReader
    private let claude: ClaudeUsageReader

    public init(
        codex: any AccountClient,
        cursor: CursorUsageReader = .init(),
        claude: ClaudeUsageReader = .init()
    ) {
        self.codex = codex
        self.cursor = cursor
        self.claude = claude
    }

    public func readUsage(
        for account: AccountProfile,
        profileHome: URL,
        refreshTokenOnUnauthorized: Bool
    ) async throws -> WeeklyUsage {
        switch account.kind {
        case .codex:
            try await codex.readWeeklyUsage(
                profileHome: profileHome,
                refreshTokenOnUnauthorized: refreshTokenOnUnauthorized
            )
        case .cursor:
            try await cursor.read(profileHome: profileHome)
        case .claude:
            try await claude.read(profileHome: profileHome)
        }
    }
}
