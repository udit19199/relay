// SPDX-License-Identifier: MIT
import Foundation

// Mirror of Relay's accounts plus their usage, refreshed from the local
// proxy's management API. Relay serves every account concurrently, so there
// is no active account, no switching, and no credential files here.
@MainActor
open class AccountController {
    public var onChange: (@MainActor () -> Void)?
    public private(set) var accounts: [RelayAccount] = [] { didSet { onChange?() } }
    public private(set) var usageStates: [String: UsageViewState] = [:] { didSet { onChange?() } }
    public private(set) var isMutating = false { didSet { onChange?() } }
    public private(set) var isAddingAccount = false { didSet { onChange?() } }
    public private(set) var addingProvider: RelayProvider? { didSet { onChange?() } }
    public private(set) var addingState: String? { didSet { onChange?() } }
    public var visibleError: OperationError? { didSet { onChange?() } }

    private let client: RelayClient
    private var hasStarted = false
    private var usageRefreshTask: Task<Void, Never>?
    private var nextUsageRefreshTask: Task<Void, Never>?
    private var addAccountTask: Task<Void, Never>?
    private var backgroundUsageRefreshInterval: Duration = .seconds(30)
    private var isBackgroundUsageRefreshEnabled = false

    public init(client: RelayClient) {
        self.client = client
    }

    // Lowest weekly remaining across accounts: the number that decides
    // whether you can keep working. Nil when nothing usable is known.
    public var headlineRemainingPercent: Int? {
        usageStates.values.compactMap(\.displayedUsage?.remainingPercent).min()
    }

    public var headlineFiveHourPercent: Int? {
        usageStates.values.compactMap(\.displayedUsage?.fiveHourRemainingPercent).min()
    }

    public func start() async {
        guard !hasStarted else { return }
        hasStarted = true
        await refreshAccounts()
    }

    public func refreshAccounts() async {
        isMutating = true
        defer { isMutating = false }
        do {
            accounts = try await client.listAccounts()
            usageStates = usageStates.filter { id, _ in accounts.contains(where: { $0.id == id }) }
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
        do {
            let listed = try await client.listAccounts()
            guard !Task.isCancelled else { return }
            let knownIDs = Set(listed.map(\.id))
            accounts = listed
            for account in listed {
                if let usage = account.relayUsage() {
                    usageStates[account.id] = .loaded(usage)
                } else if usageStates[account.id]?.displayedUsage != nil {
                    usageStates[account.id] = .unavailable("Relay returned no weekly usage window for this account.")
                } else {
                    usageStates[account.id] = .unavailable("Relay returned no weekly usage window for this account.")
                }
            }
            usageStates = usageStates.filter { knownIDs.contains($0.key) }
        } catch {
            guard !Task.isCancelled else { return }
            for account in accounts {
                if let cached = usageStates[account.id]?.displayedUsage {
                    usageStates[account.id] = .stale(cached, error.localizedDescription)
                } else {
                    usageStates[account.id] = .unavailable(error.localizedDescription)
                }
            }
            if accounts.isEmpty {
                showError(error)
            }
        }
    }

    // Opens Relay's OAuth URL in the browser; call openURL with the result,
    // then the poll below completes the flow and refreshes the list.
    public func beginAddAccount(provider: RelayProvider) async -> URL? {
        guard !isMutating, !isAddingAccount else { return nil }
        visibleError = nil
        isAddingAccount = true
        addingProvider = provider
        do {
            let (url, state) = try await client.startAddAccount(provider: provider)
            addingState = state
            addAccountTask = Task { [weak self] in
                guard let self else { return }
                await self.pollAddAccount(state: state)
                self.isAddingAccount = false
                self.addingProvider = nil
                self.addingState = nil
                self.addAccountTask = nil
            }
            return url
        } catch {
            isAddingAccount = false
            addingProvider = nil
            showError(error)
            return nil
        }
    }

    public func cancelAddingAccount() {
        let state = addingState
        addAccountTask?.cancel()
        addAccountTask = nil
        isAddingAccount = false
        addingProvider = nil
        addingState = nil
        if let state {
            Task { [client] in await client.cancelAddAccount(state: state) }
        }
    }

    private func pollAddAccount(state: String) async {
        let deadline = Date().addingTimeInterval(5 * 60)
        while Date() < deadline {
            if Task.isCancelled { return }
            do {
                if try await client.pollAddAccount(state: state) == .done {
                    await refreshAccounts()
                    refreshWeeklyUsage()
                    return
                }
            } catch {
                showError(error)
                return
            }
            try? await Task.sleep(for: .seconds(2))
        }
        showError(RelayError.serverMessage("Sign-in timed out after 5 minutes. Try again."))
    }

    public func removeAccount(id: String) async {
        guard let account = accounts.first(where: { $0.id == id }), !isMutating else { return }
        isMutating = true
        await cancelUsageRefresh()
        defer { isMutating = false; refreshWeeklyUsage() }
        do {
            try await client.removeAccount(name: account.name)
            await refreshAccounts()
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

    private func showError(_ error: any Error) {
        visibleError = OperationError(
            title: "Operation failed",
            message: error.localizedDescription,
            underlyingDescription: String(describing: error)
        )
    }
}
