// SPDX-License-Identifier: MIT
import Foundation

private enum UsageRefreshResult: Sendable {
    case success(UUID, WeeklyUsage)
    case failure(UUID, String)
}

@MainActor
open class AccountController {
    public var onChange: (@MainActor () -> Void)?
    public private(set) var accounts: [AccountProfile] = [] { didSet { onChange?() } }
    public private(set) var activeAccountID: UUID? { didSet { onChange?() } }
    public private(set) var usageStates: [UUID: UsageViewState] = [:] { didSet { onChange?() } }
    public private(set) var isMutating = false { didSet { onChange?() } }
    public private(set) var isAddingAccount = false { didSet { onChange?() } }
    public private(set) var awaitingBrowserSession: AccountKind? { didSet { onChange?() } }
    public var visibleError: OperationError? { didSet { onChange?() } }

    private let store: AccountStore
    private let codex: any AccountClient
    private let usageService: ProfileUsageService
    private let switchService: any SwitchServicing
    private var hasStarted = false
    private var usageRefreshTask: Task<Void, Never>?
    private var nextUsageRefreshTask: Task<Void, Never>?
    private var addAccountTask: Task<Void, Never>?
    private var pendingProviderProfileID: UUID?
    private var backgroundUsageRefreshInterval: Duration = .seconds(30)
    private var isBackgroundUsageRefreshEnabled = false

    public init(
        store: AccountStore,
        codex: any AccountClient,
        usageService: ProfileUsageService? = nil,
        switchService: any SwitchServicing
    ) {
        self.store = store
        self.codex = codex
        self.usageService = usageService ?? ProfileUsageService(codex: codex)
        self.switchService = switchService
    }

    public var activeRemainingPercent: Int? {
        guard let activeAccountID else { return nil }
        return usageStates[activeAccountID]?.displayedUsage?.remainingPercent
    }

    public func start() async {
        guard !hasStarted else { return }
        hasStarted = true
        isMutating = true
        defer { isMutating = false }
        do {
            var registry = try await store.loadRegistry()
            if registry.accounts.isEmpty, await store.activeCredentialExists() {
                // Import the live Codex login only; Cursor and Claude profiles are added manually for now.
                let activeHome = await store.activeCodexHome()
                let identity = try await codex.readIdentity(profileHome: activeHome)
                let profile = AccountProfile(
                    id: UUID(),
                    kind: .codex,
                    displayName: identity.suggestedDisplayName,
                    email: identity.email,
                    accountID: identity.accountID,
                    createdAt: Date(),
                    lastUsedAt: Date()
                )
                try await store.importCurrentProfile(profile)
                registry = try await store.loadRegistry()
            }
            apply(registry)
            do {
                apply(try await store.loadUsageCache())
            } catch {
                showError(error)
            }
        } catch {
            showError(error)
        }
    }

    public func refreshWeeklyUsage() {
        if isBackgroundUsageRefreshEnabled {
            scheduleNextWeeklyUsageRefresh()
        }
        guard !isMutating, !accounts.isEmpty, usageRefreshTask == nil else { return }
        usageRefreshTask = Task { [weak self] in
            guard let self else { return }
            await self.performWeeklyUsageRefresh()
            self.usageRefreshTask = nil
        }
    }

    public func waitForWeeklyUsageRefresh() async {
        await usageRefreshTask?.value
    }

    public func startBackgroundUsageRefresh(every interval: Duration = .seconds(30)) async {
        backgroundUsageRefreshInterval = interval
        isBackgroundUsageRefreshEnabled = true
        await start()
        refreshWeeklyUsage()
    }

    public func stopBackgroundUsageRefresh() {
        isBackgroundUsageRefreshEnabled = false
        nextUsageRefreshTask?.cancel()
        nextUsageRefreshTask = nil
    }

    private func scheduleNextWeeklyUsageRefresh() {
        nextUsageRefreshTask?.cancel()
        let interval = backgroundUsageRefreshInterval
        nextUsageRefreshTask = Task { [weak self] in
            do {
                try await Task.sleep(for: interval)
            } catch {
                return
            }
            guard !Task.isCancelled, let self else { return }
            self.nextUsageRefreshTask = nil
            self.refreshWeeklyUsage()
        }
    }

    private func performWeeklyUsageRefresh() async {
        guard !Task.isCancelled else { return }
        if let id = activeAccountID,
           let active = accounts.first(where: { $0.id == id }),
           active.kind == .codex {
            do {
                _ = try await store.syncActiveCredentialIfMatching(id: id)
                guard !Task.isCancelled else { return }
            } catch {
                guard !Task.isCancelled else { return }
                showError(error)
                return
            }
        }
        let targets = await withTaskGroup(of: (UUID, URL).self, returning: [(UUID, URL)].self) { group in
            for account in accounts {
                group.addTask { [store] in
                    (account.id, await store.profileHome(id: account.id))
                }
            }
            var values: [(UUID, URL)] = []
            for await value in group { values.append(value) }
            return values
        }

        guard !Task.isCancelled else { return }
        let activeID = activeAccountID
        let accountByID = Dictionary(uniqueKeysWithValues: accounts.map { ($0.id, $0) })
        await withTaskGroup(of: UsageRefreshResult.self) { group in
            for (id, home) in targets {
                guard let account = accountByID[id] else { continue }
                group.addTask { [usageService] in
                    do {
                        let usage = try await usageService.readUsage(
                            for: account,
                            profileHome: home,
                            refreshTokenOnUnauthorized: id != activeID
                        )
                        return .success(id, usage)
                    } catch {
                        return .failure(id, error.localizedDescription)
                    }
                }
            }
            for await result in group {
                guard !Task.isCancelled else { continue }
                switch result {
                case let .success(id, usage):
                    guard accounts.contains(where: { $0.id == id }) else { continue }
                    usageStates[id] = .loaded(usage)
                    do {
                        try await store.cacheWeeklyUsage(usage, profileID: id)
                    } catch {
                        showError(error)
                    }
                case let .failure(id, message):
                    guard accounts.contains(where: { $0.id == id }) else { continue }
                    if let cached = usageStates[id]?.displayedUsage {
                        usageStates[id] = .stale(cached, message)
                    } else {
                        usageStates[id] = .unavailable(message)
                    }
                }
            }
        }
    }

    public func switchAccount(to id: UUID) async {
        guard let account = accounts.first(where: { $0.id == id }),
              account.supportsAccountSwitch else { return }
        guard id != activeAccountID, !isMutating else { return }
        isMutating = true
        await cancelUsageRefresh()
        defer { isMutating = false; refreshWeeklyUsage() }
        do {
            try await switchService.switchAccount(to: id)
            apply(try await store.loadRegistry())
        } catch let error as OperationError {
            visibleError = error
        } catch {
            showError(error)
        }
    }

    public func addAccount(kind: AccountKind) {
        guard !isMutating, !isAddingAccount, awaitingBrowserSession == nil else { return }
        visibleError = nil
        switch kind {
        case .codex:
            isAddingAccount = true
            addAccountTask = Task { [weak self] in
                guard let self else { return }
                await self.performCodexAddAccount()
                self.isAddingAccount = false
                self.addAccountTask = nil
            }
        case .cursor, .claude:
            break
        }
    }

    public func cancelAddingAccount() {
        addAccountTask?.cancel()
        cancelBrowserSignIn()
    }

    public func beginBrowserSignIn(kind: AccountKind) async -> Bool {
        guard kind != .codex else { return false }
        do {
            let id = UUID()
            _ = try await store.createProfileDirectory(id: id)
            pendingProviderProfileID = id
            awaitingBrowserSession = kind
            return true
        } catch {
            showError(error)
            return false
        }
    }

    public func cancelBrowserSignIn() {
        let id = pendingProviderProfileID
        pendingProviderProfileID = nil
        awaitingBrowserSession = nil
        if let id {
            Task { [weak self] in
                try? await self?.store.discardUnregisteredProfile(id: id)
            }
        }
    }

    public func finishBrowserSession(pasted: String) async {
        guard let kind = awaitingBrowserSession, let id = pendingProviderProfileID else { return }
        guard let credential = ProviderSessionCredential.parsePasted(pasted, kind: kind) else {
            showError(ProviderUsageError.missingSession)
            return
        }
        do {
            let home = await store.profileHome(id: id)
            try credential.save(to: home)
            let resolved = try await ProviderIdentityResolver.resolve(kind: kind, credential: credential)
            let profile = AccountProfile(
                id: id,
                kind: kind,
                displayName: resolved.suggestedDisplayName,
                email: resolved.email,
                accountID: resolved.accountID,
                createdAt: Date()
            )
            try await store.addProfile(profile)
            pendingProviderProfileID = nil
            awaitingBrowserSession = nil
            apply(try await store.loadRegistry())
            refreshWeeklyUsage()
        } catch {
            showError(error)
        }
    }

    public func redeemBankedReset(for id: UUID) async {
        guard let account = accounts.first(where: { $0.id == id }),
              account.supportsBankedReset else { return }
        guard !isMutating else { return }
        isMutating = true
        await cancelUsageRefresh()
        defer { isMutating = false; refreshWeeklyUsage() }
        visibleError = nil
        do {
            try await codex.redeemBankedReset(profileHome: await store.profileHome(id: id))
        } catch {
            showError(error)
        }
    }

    private func performCodexAddAccount() async {
        let id = UUID()
        do {
            let home = try await store.createProfileDirectory(id: id)
            try Task.checkCancellation()
            let identity = try await codex.login(profileHome: home)
            try Task.checkCancellation()
            let profile = AccountProfile(
                id: id,
                kind: .codex,
                displayName: identity.suggestedDisplayName,
                email: identity.email,
                accountID: identity.accountID,
                createdAt: Date(),
                lastUsedAt: nil
            )
            try await store.addProfile(profile)
            apply(try await store.loadRegistry())
        } catch {
            let loginError = error
            do {
                try await store.discardUnregisteredProfile(id: id)
            } catch {
                showError(NSError(domain: "Sidekick.Login", code: 1, userInfo: [
                    NSLocalizedDescriptionKey: "\(loginError.localizedDescription) Removing the incomplete profile also failed: \(error.localizedDescription)",
                ]))
                return
            }
            if !(loginError is CancellationError) { showError(loginError) }
        }
    }

    public func removeAccount(id: UUID) async {
        guard !isMutating else { return }
        isMutating = true
        await cancelUsageRefresh()
        defer { isMutating = false; refreshWeeklyUsage() }
        do {
            try await store.removeAccount(id: id)
            apply(try await store.loadRegistry())
            usageStates[id] = nil
        } catch {
            showError(error)
        }
    }

    public func dismissError() {
        visibleError = nil
    }

    private func cancelUsageRefresh() async {
        usageRefreshTask?.cancel()
        await usageRefreshTask?.value
    }

    private func apply(_ registry: AccountRegistry) {
        accounts = registry.accounts
        activeAccountID = registry.activeAccountID
        usageStates = usageStates.filter { id, _ in registry.accounts.contains(where: { $0.id == id }) }
    }

    private func apply(_ cache: UsageCache) {
        let validAccountIDs = Set(accounts.map(\.id))
        for entry in cache.entries where validAccountIDs.contains(entry.profileID) {
            usageStates[entry.profileID] = .loaded(entry.usage)
        }
    }

    private func showError(_ error: any Error) {
        visibleError = OperationError(
            stage: nil,
            title: "Operation failed",
            message: error.localizedDescription,
            underlyingDescription: String(describing: error)
        )
    }
}
