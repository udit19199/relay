// SPDX-License-Identifier: MIT
import Foundation
import Testing
@testable import SidekickCore

@MainActor
struct AccountControllerTests {
    @Test func firstActivationInstallsAndCommitsTheSavedAccount() async throws {
        let fixture = try ControllerFixture()
        defer { fixture.clean() }
        let id = UUID()
        let profile = AccountProfile(id: id, displayName: "Demo", email: "demo@example.test", accountID: "demo-account", createdAt: Date())
        let home = try await fixture.store.createProfileDirectory(id: id)
        try Data("saved-fixture".utf8).write(to: home.appendingPathComponent("auth.json"))
        try await fixture.store.addProfile(profile)
        let service = SwitchService(store: fixture.store, codex: fixture.client)
        try await service.switchAccount(to: id)
        #expect(try await fixture.store.loadRegistry().activeAccountID == id)
        #expect(try String(contentsOf: fixture.active.appendingPathComponent("auth.json"), encoding: .utf8) == "saved-fixture")
    }

    @Test func failedFirstActivationRestoresTheUnsignedInState() async throws {
        let fixture = try ControllerFixture()
        defer { fixture.clean() }
        let id = UUID()
        let profile = AccountProfile(id: id, displayName: "Other", email: "other@example.test", accountID: "other", createdAt: Date())
        let home = try await fixture.store.createProfileDirectory(id: id)
        try Data("saved-other-fixture".utf8).write(to: home.appendingPathComponent("auth.json"))
        try await fixture.store.addProfile(profile)
        let service = SwitchService(store: fixture.store, codex: fixture.client)
        do { try await service.switchAccount(to: id); Issue.record("Identity mismatch should fail.") }
        catch { #expect((error as? OperationError)?.stage == .verifyTargetIdentity) }
        #expect(try await fixture.store.loadRegistry().activeAccountID == nil)
        #expect(await !fixture.store.activeCredentialExists())
        #expect(try String(contentsOf: home.appendingPathComponent("auth.json"), encoding: .utf8) == "saved-other-fixture")
    }

    @Test func startupImportsCurrentLoginOnce() async throws {
        let fixture = try ControllerFixture()
        defer { fixture.clean() }
        try fixture.writeActiveCredential()
        await fixture.model.start()
        await fixture.model.start()
        #expect(fixture.model.accounts.count == 1)
        #expect(fixture.model.activeAccountID == fixture.model.accounts.first?.id)
        let id = try #require(fixture.model.accounts.first?.id)
        let saved = await fixture.store.profileHome(id: id)
        let imported = try Data(contentsOf: saved.appendingPathComponent("auth.json"))
        let live = try Data(contentsOf: fixture.active.appendingPathComponent("auth.json"))
        #expect(imported == live)
    }

    @Test func emptyStartupDoesNotCreateAnAccount() async throws {
        let fixture = try ControllerFixture()
        defer { fixture.clean() }
        await fixture.model.start()
        #expect(fixture.model.accounts.isEmpty)
        #expect(fixture.model.visibleError == nil)
    }

    @Test func refreshKeepsLastGoodUsageWhenTheServerFails() async throws {
        let fixture = try ControllerFixture()
        defer { fixture.clean() }
        try fixture.writeActiveCredential()
        await fixture.model.start()
        fixture.model.refreshWeeklyUsage()
        await fixture.model.waitForWeeklyUsageRefresh()
        let id = try #require(fixture.model.activeAccountID)
        #expect(fixture.model.usageStates[id]?.displayedUsage?.remainingPercent == 72)
        await fixture.client.failUsage()
        fixture.model.refreshWeeklyUsage()
        await fixture.model.waitForWeeklyUsageRefresh()
        #expect(fixture.model.usageStates[id]?.displayedUsage?.remainingPercent == 72)
        #expect(fixture.model.usageStates[id]?.refreshError != nil)
        #expect(try await fixture.store.loadUsageCache().entries.first?.usage.remainingPercent == 72)
    }

    @Test func activeProfileCannotBeRemoved() async throws {
        let fixture = try ControllerFixture()
        defer { fixture.clean() }
        try fixture.writeActiveCredential()
        await fixture.model.start()
        let id = try #require(fixture.model.activeAccountID)
        await fixture.model.removeAccount(id: id)
        #expect(fixture.model.accounts.count == 1)
        #expect(fixture.model.visibleError != nil)
    }

}

@MainActor
private struct ControllerFixture {
    let root: URL
    let active: URL
    let store: AccountStore
    let client: FixtureClient
    let model: AccountController
    init() throws {
        root = FileManager.default.temporaryDirectory.appendingPathComponent("sidekick-core-test-\(UUID())")
        active = root.appendingPathComponent("active")
        try FileManager.default.createDirectory(at: active, withIntermediateDirectories: true)
        store = AccountStore(baseURL: root.appendingPathComponent("store"), activeHomeURL: active)
        client = FixtureClient()
        model = AccountController(store: store, codex: client,
            switchService: SwitchService(store: store, codex: client))
    }
    func writeActiveCredential() throws {
        try Data(#"{"tokens":{"account_id":"demo-account","access_token":"fixture-secret-token"}}"#.utf8)
            .write(to: active.appendingPathComponent("auth.json"))
    }
    func clean() { try? FileManager.default.removeItem(at: root) }
}

private actor FixtureClient: AccountClient {
    private var usageFails = false
    func failUsage() { usageFails = true }
    func readIdentity(profileHome: URL) async throws -> AccountIdentity {
        AccountIdentity(accountID: "demo-account", email: "demo@example.test")
    }
    func readWeeklyUsage(profileHome: URL) async throws -> WeeklyUsage {
        if usageFails { throw CodexClientError.connectionClosed }
        return WeeklyUsage(remainingPercent: 72, resetsAt: Date(timeIntervalSince1970: 2_000_000_000))
    }
    func redeemBankedReset(profileHome: URL) async throws {}
    func login(profileHome: URL) async throws -> AccountIdentity {
        throw CodexClientError.loginFailed("Fixture login is disabled.")
    }
}
