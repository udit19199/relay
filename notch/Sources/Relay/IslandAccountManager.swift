// SPDX-License-Identifier: MIT
import RelayCore
import AppKit
import SwiftUI

// Relay's account list and usage, inside the notch island.
// Relay serves every account concurrently: rows are display-only,
// Add Account runs Relay's browser OAuth, Remove deletes the credential.
struct IslandAccountManager: View {
    @ObservedObject var model: AppModel
    @Binding var showsCompactUsage: Bool
    let onDismiss: () -> Void
    var onConfirmationChanged: (Bool) -> Void = { _ in }
    var onExpandedBodyLayoutChanged: (IslandExpandedBodyLayout) -> Void = { _ in }
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var removingAccount: RelayAccount?
    @State private var addAccountFlow: AddAccountFlow?
    // Measured heights that size the expanded panel to its content.
    @State private var bannerHeight: CGFloat = 0
    @State private var listHeight: CGFloat = 0
    @State private var footerHeight: CGFloat = 0

    private var isManagingAddAccount: Bool {
        model.isAddingAccount || addAccountFlow != nil
    }

    var body: some View {
        VStack(spacing: 0) {
            if let error = model.visibleError {
                VStack(spacing: 0) {
                    InlineErrorBanner(
                        title: error.title,
                        message: error.message,
                        dismissTitle: "OK",
                        onDismiss: model.dismissError
                    )
                    Divider()
                        .padding(.horizontal, 16)
                }
                .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { bannerHeight = $0 }
            }

            Group {
                if let addAccountFlow {
                    addAccountPanel(flow: addAccountFlow)
                        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
                        .transition(.opacity)
                } else {
                    ScrollView {
                        accountList
                            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { listHeight = $0 }
                    }
                    .frame(maxHeight: .infinity, alignment: .top)
                    .scrollIndicators(.hidden)
                    .transition(.opacity)
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
            .animation(reduceMotion ? nil : .easeInOut(duration: 0.2), value: addAccountFlow != nil)

            VStack(spacing: 0) {
            Divider()
                .padding(.horizontal, 16)

            HStack(spacing: 2) {
                accountActions
                Button {
                    showsCompactUsage.toggle()
                } label: {
                    Image(systemName: showsCompactUsage ? "eye" : "eye.slash")
                        .font(.system(size: 12, weight: .medium))
                        .frame(width: 30, height: 30)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Show compact usage")
                .accessibilityValue(showsCompactUsage ? "On" : "Off")
                .help(showsCompactUsage ? "Hide compact usage. Hover over the notch to reopen Relay." : "Show compact usage")
                FooterAction(
                    title: "Quit",
                    systemImage: "power",
                    shortcut: KeyboardShortcut("q", modifiers: .command)
                ) {
                    NSApp.terminate(nil)
                }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 8)
            }
            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { footerHeight = $0 }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        .onAppear { reportExpandedBodyLayout(animated: false) }
        .onChange(of: addAccountFlow) { _, _ in reportExpandedBodyLayout(animated: false) }
        .onChange(of: model.visibleError == nil) { _, isHidden in
            if isHidden { bannerHeight = 0 }
        }
        .onChange(of: [bannerHeight, listHeight, footerHeight]) { _, _ in
            if addAccountFlow == nil { reportExpandedBodyLayout(animated: true) }
        }
        .onChange(of: model.isAddingAccount) { _, isAdding in
            if !isAdding, addAccountFlow != nil {
                setAddAccountFlow(nil)
                Task { await model.refreshAccounts() }
            }
        }
        .onChange(of: removingAccount != nil) { _, isPresented in
            onConfirmationChanged(isPresented)
        }
        .confirmationDialog(
            removingAccount.map { "Remove \($0.displayName)?" } ?? "Remove account",
            isPresented: Binding(
                get: { removingAccount != nil },
                set: { if !$0 { removingAccount = nil } }
            ),
            titleVisibility: .visible
        ) {
            if let account = removingAccount {
                Button("Remove", role: .destructive) {
                    removingAccount = nil
                    Task { await model.removeAccount(id: account.id) }
                }
                .disabled(model.isMutating)
            }
            Button("Cancel", role: .cancel) { removingAccount = nil }
        } message: {
            Text("This deletes the credential from Relay. Devices using it lose access.")
        }
    }

    @ViewBuilder
    private func addAccountPanel(flow: AddAccountFlow) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            switch flow {
            case .chooseProvider:
                VStack(alignment: .leading, spacing: 10) {
                    Text("Add which account?")
                        .font(.headline)
                        .islandNonSelectableText()
                    Text("Opens Relay's sign-in in your browser. Finish there; the account appears here.")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                        .islandNonSelectableText()
                }
                Spacer(minLength: 20)
                VStack(spacing: 10) {
                    ForEach(RelayProvider.allCases, id: \.self) { provider in
                        providerButton(provider.displayName, systemImage: provider.systemImage) {
                            beginAddAccount(provider: provider)
                        }
                    }
                }
                Spacer(minLength: 20)
                Button("Cancel", role: .cancel) { cancelAddAccountFlow() }
                    .buttonStyle(.link)
                    .frame(maxWidth: .infinity, alignment: .center)
            case let .waiting(provider):
                VStack(alignment: .leading, spacing: 10) {
                    Text("Finish \(provider.displayName) sign-in")
                        .font(.headline)
                        .islandNonSelectableText()
                    Text("Complete sign-in in your browser. This panel closes when Relay saves the account.")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                        .islandNonSelectableText()
                }
                Spacer(minLength: 12)
                ProgressView()
                    .controlSize(.small)
                Spacer(minLength: 12)
                Button("Cancel Sign-In", role: .cancel) { cancelAddAccountFlow() }
                    .buttonStyle(.link)
                    .frame(maxWidth: .infinity, alignment: .center)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 16)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }

    private func providerButton(
        _ title: String,
        systemImage: String,
        action: @escaping () -> Void
    ) -> some View {
        Button(action: action) {
            Label(title, systemImage: systemImage)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .buttonStyle(.bordered)
        .controlSize(.large)
    }

    private func beginAddAccount(provider: RelayProvider) {
        Task {
            guard let url = await model.beginAddAccount(provider: provider) else {
                setAddAccountFlow(nil)
                return
            }
            NSWorkspace.shared.open(url)
            setAddAccountFlow(.waiting(provider))
        }
    }

    private func cancelAddAccountFlow() {
        setAddAccountFlow(nil)
        model.cancelAddingAccount()
    }

    private func setAddAccountFlow(_ flow: AddAccountFlow?) {
        addAccountFlow = flow
        reportExpandedBodyLayout(animated: false)
    }

    private func reportExpandedBodyLayout(animated: Bool) {
        let layout: IslandExpandedBodyLayout
        if let addAccountFlow {
            layout = .addContent(layoutPhase(for: addAccountFlow))
        } else {
            // Keep the last size until the list and footer have been measured.
            guard listHeight > 0, footerHeight > 0 else { return }
            layout = .accounts(contentHeight: bannerHeight + listHeight + footerHeight)
        }
        if animated {
            onExpandedBodyLayoutChanged(layout)
        } else {
            var transaction = Transaction()
            transaction.disablesAnimations = true
            withTransaction(transaction) {
                onExpandedBodyLayoutChanged(layout)
            }
        }
    }

    private func layoutPhase(for flow: AddAccountFlow) -> IslandExpandedBodyLayout.AddAccountLayoutPhase {
        switch flow {
        case .chooseProvider: .chooseProvider
        case .waiting: .waitingForBrowser
        }
    }

    private var accountList: some View {
        VStack(spacing: 0) {
            if model.accounts.isEmpty {
                Text("No saved accounts")
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 28)
            } else {
                VStack(spacing: 6) {
                    if model.accounts.count > 1 {
                        TimelineView(.periodic(from: .now, by: 30)) { context in
                            PoolSummaryRow(
                                pool: model.pooledUsage(at: context.date),
                                accountCount: model.accounts.filter { !$0.disabled }.count,
                                now: context.date
                            )
                        }
                    }
                    ForEach(model.accounts) { account in
                        VStack(alignment: .leading, spacing: 0) {
                            TimelineView(.periodic(from: .now, by: 30)) { context in
                                AccountRow(
                                    account: account,
                                    usageState: model.usageStates[account.id] ?? .idle,
                                    now: context.date
                                )
                            }
                            if account.disabled || account.unavailable {
                                Text(verbatim: account.statusSummary)
                                    .font(.system(size: 10.5))
                                    .foregroundStyle(.orange)
                                    .lineLimit(2)
                                    .help(account.statusMessage ?? account.statusSummary)
                                    .padding(.horizontal, 12)
                                    .padding(.bottom, 8)
                            }
                        }
                        .contextMenu {
                            Button(role: .destructive) {
                                removingAccount = account
                            } label: {
                                Text("Remove \(account.displayName)?")
                            }
                        }
                    }
                }
                .padding(.vertical, 8)
            }
        }
    }

    private var accountActions: some View {
        FooterAction(
            title: isManagingAddAccount ? "Cancel Sign-In" : "Add Account",
            systemImage: isManagingAddAccount ? "xmark" : "plus",
            showsProgress: isManagingAddAccount
        ) {
            if isManagingAddAccount {
                cancelAddAccountFlow()
            } else {
                setAddAccountFlow(.chooseProvider)
            }
        }
        .disabled(model.isMutating && !isManagingAddAccount)
    }
}

private extension View {
    func islandNonSelectableText() -> some View {
        textSelection(.disabled)
    }
}

private enum AddAccountFlow: Equatable {
    case chooseProvider
    case waiting(RelayProvider)
}

private struct InlineErrorBanner: View {
    let title: String
    let message: String
    let dismissTitle: String
    let onDismiss: () -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Image(systemName: "exclamationmark.triangle.fill")
                .font(.system(size: 11))
                .foregroundStyle(.orange)
                .padding(.top, 2)

            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.system(size: 11.5, weight: .semibold))
                Text(message)
                    .font(.system(size: 10.5))
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }

            Spacer(minLength: 4)

            Button(action: onDismiss) {
                Image(systemName: "xmark")
                    .font(.system(size: 9, weight: .semibold))
                    .frame(width: 22, height: 22)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(dismissTitle)
            .help(dismissTitle)
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 8)
        .background(Color.orange.opacity(0.08))
    }
}

private struct FooterAction: View {
    let title: String
    let systemImage: String
    var shortcut: KeyboardShortcut?
    var showsProgress = false
    let action: () -> Void

    @State private var isHovering = false

    init(
        title: String,
        systemImage: String,
        shortcut: KeyboardShortcut? = nil,
        showsProgress: Bool = false,
        action: @escaping () -> Void
    ) {
        self.title = title
        self.systemImage = systemImage
        self.shortcut = shortcut
        self.showsProgress = showsProgress
        self.action = action
    }

    var body: some View {
        Button(action: action) {
            HStack(spacing: 6) {
                Label(title, systemImage: systemImage)
                if showsProgress {
                    ProgressView()
                        .controlSize(.small)
                }
            }
            .font(.system(size: 12, weight: .medium))
            .lineLimit(1)
            .minimumScaleFactor(0.80)
            .padding(.horizontal, 5)
            .frame(maxWidth: .infinity, minHeight: 30)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .keyboardShortcut(shortcut)
        .frame(maxWidth: .infinity)
        .background(
            Color.primary.opacity(isHovering ? 0.06 : 0),
            in: RoundedRectangle(cornerRadius: 7, style: .continuous)
        )
        .onHover { isHovering = $0 }
    }
}
