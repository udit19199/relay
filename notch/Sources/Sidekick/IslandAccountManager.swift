// SPDX-License-Identifier: MIT
import SidekickCore
import AppKit
import SwiftUI

struct IslandAccountManager: View {
    @ObservedObject var model: AppModel
    @Binding var showsCompactUsage: Bool
    let onDismiss: () -> Void
    var onConfirmationChanged: (Bool) -> Void = { _ in }
    var onExpandedBodyLayoutChanged: (IslandExpandedBodyLayout) -> Void = { _ in }
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var confirmation: AccountConfirmation?
    @State private var addAccountFlow: AddAccountFlow?
    @State private var pastedSessionText = ""
    @State private var hoveredAccountID: UUID?
    @State private var hoveredResetAccountID: UUID?

    private var isManagingAddAccount: Bool {
        model.isAddingAccount || model.awaitingBrowserSession != nil || addAccountFlow != nil
    }

    var body: some View {
        VStack(spacing: 0) {
            if let error = model.visibleError {
                InlineErrorBanner(
                    title: error.title,
                    message: error.message,
                    dismissTitle: "OK",
                    onDismiss: model.dismissError
                )
                Divider()
                    .padding(.horizontal, 16)
            }

            Group {
                if let addAccountFlow {
                    addAccountPanel(flow: addAccountFlow)
                        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
                        .transition(.opacity)
                } else {
                    ScrollView {
                        accountList
                    }
                    .frame(maxHeight: .infinity, alignment: .top)
                    .scrollIndicators(.hidden)
                    .transition(.opacity)
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
            .animation(reduceMotion ? nil : .easeInOut(duration: 0.2), value: addAccountFlow != nil)

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
                .help(showsCompactUsage ? "Hide compact usage. Hover over the notch to reopen Sidekick." : "Show compact usage")
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
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        .onAppear { reportExpandedBodyLayout(animated: false) }
        .onChange(of: addAccountFlow) { _, _ in reportExpandedBodyLayout(animated: false) }
        .onChange(of: model.accounts.count) { _, _ in
            if addAccountFlow == nil { reportExpandedBodyLayout(animated: false) }
        }
        .onChange(of: confirmation != nil) { _, isPresented in
            onConfirmationChanged(isPresented)
        }
        .confirmationDialog(
            confirmation?.title ?? "Confirm account action",
            isPresented: Binding(
                get: { confirmation != nil },
                set: { if !$0 { confirmation = nil } }
            ),
            titleVisibility: .visible
        ) {
            confirmationButtons
        } message: {
            if let confirmation { Text(confirmation.message(model: model)) }
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
                    Text("Codex uses the Codex sign-in flow. Cursor and Claude open your default browser to sign in.")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                        .islandNonSelectableText()
                }
                Spacer(minLength: 20)
                VStack(spacing: 10) {
                    providerButton("Codex", systemImage: "terminal") {
                        setAddAccountFlow(nil)
                        model.addAccount(kind: .codex)
                    }
                    providerButton("Cursor", systemImage: "cursorarrow.rays") {
                        startBrowserSignIn(kind: .cursor)
                    }
                    providerButton("Claude", systemImage: "bubble.left.and.bubble.right") {
                        startBrowserSignIn(kind: .claude)
                    }
                }
                Spacer(minLength: 20)
                Button("Cancel", role: .cancel) { cancelAddAccountFlow() }
                    .buttonStyle(.link)
                    .frame(maxWidth: .infinity, alignment: .center)
            case let .pasteSession(kind):
                VStack(alignment: .leading, spacing: 10) {
                    Text("Finish \(kind.displayName) sign-in")
                        .font(.headline)
                        .islandNonSelectableText()
                    Text(kind.sessionPasteInstructions)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                        .islandNonSelectableText()
                }
                Spacer(minLength: 12)
                TextField("Session cookie", text: $pastedSessionText, axis: .vertical)
                    .textFieldStyle(.roundedBorder)
                    .font(.system(.caption, design: .monospaced))
                    .lineLimit(4 ... 8)
                Spacer(minLength: 12)
                HStack(spacing: 8) {
                    Button("Save Account") {
                        let text = pastedSessionText
                        Task {
                            await model.finishBrowserSession(pasted: text)
                            if model.awaitingBrowserSession == nil {
                                cancelAddAccountFlow()
                            }
                        }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(pastedSessionText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)

                    Button("Open Browser Again") {
                        SystemBrowser.openSignIn(for: kind)
                    }
                    .buttonStyle(.bordered)
                }
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

    private func startBrowserSignIn(kind: AccountKind) {
        Task {
            guard await model.beginBrowserSignIn(kind: kind) else { return }
            SystemBrowser.openSignIn(for: kind)
            pastedSessionText = ""
            setAddAccountFlow(.pasteSession(kind))
        }
    }

    private func cancelAddAccountFlow() {
        setAddAccountFlow(nil)
        pastedSessionText = ""
        model.cancelAddingAccount()
    }

    private func setAddAccountFlow(_ flow: AddAccountFlow?) {
        addAccountFlow = flow
        reportExpandedBodyLayout(animated: false)
    }

    private func reportExpandedBodyLayout(animated: Bool) {
        let layout: IslandExpandedBodyLayout =
            if let addAccountFlow {
                .addContent(layoutPhase(for: addAccountFlow))
            } else {
                .accounts(count: model.accounts.count)
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
        case .pasteSession: .pasteSession
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
                    ForEach(model.accounts) { account in
                        ZStack(alignment: .topTrailing) {
                            Button {
                                if !account.supportsAccountSwitch || account.id == model.activeAccountID {
                                    onDismiss()
                                } else {
                                    confirmation = .switchAccount(account)
                                }
                            } label: {
                                TimelineView(.periodic(from: .now, by: 30)) { context in
                                    AccountRow(
                                        account: account,
                                        usageState: model.usageStates[account.id] ?? .idle,
                                        now: context.date,
                                        isActive: account.id == model.activeAccountID,
                                        hasResetControl: account.supportsBankedReset
                                            && model.usageStates[account.id]?.displayedUsage?.availableResetCount != nil
                                    )
                                }
                            }
                            .buttonStyle(.plain)
                            .frame(maxWidth: .infinity)
                            .disabled(model.isMutating)

                            if account.supportsBankedReset,
                               let usage = model.usageStates[account.id]?.displayedUsage,
                               let count = usage.availableResetCount {
                                Button {
                                    confirmation = .bankedReset(account)
                                } label: {
                                    Text("\(count) \(count == 1 ? "reset" : "resets")")
                                        .font(.system(size: 11, weight: .medium).monospacedDigit())
                                        .foregroundStyle(.secondary)
                                        .frame(width: 64, height: 28)
                                        .background(.white.opacity(hoveredResetAccountID == account.id ? 0.10 : 0.04), in: RoundedRectangle(cornerRadius: 6))
                                        .animation(reduceMotion ? nil : .easeOut(duration: 0.12), value: hoveredResetAccountID == account.id)
                                }
                                .buttonStyle(.plain)
                                .onHover { isHovering in
                                    if isHovering {
                                        hoveredResetAccountID = account.id
                                    } else if hoveredResetAccountID == account.id {
                                        hoveredResetAccountID = nil
                                    }
                                }
                                .accessibilityLabel("Use a banked usage reset for \(account.displayName)")
                                .accessibilityValue("\(count) available")
                                .help(count == 0 ? "No banked resets available" : resetExpiryHelp(usage: usage))
                                .disabled(model.isMutating || count == 0)
                                .padding(.top, 12)
                                .padding(.trailing, 12)
                            }
                        }
                        .onHover { isHovering in
                            if isHovering {
                                hoveredAccountID = account.id
                            } else if hoveredAccountID == account.id {
                                hoveredAccountID = nil
                            }
                        }
                        .background(accountRowBackground(for: account))
                        .contextMenu {
                            if account.id != model.activeAccountID {
                                Button(role: .destructive) {
                                    confirmation = .remove(account)
                                } label: {
                                    Text("Remove \(account.displayName)?")
                                }
                            }
                        }
                    }
                }
                .padding(.vertical, 8)
            }

        }
    }

    private func accountRowBackground(for account: AccountProfile) -> some View {
        let color = if account.id == model.activeAccountID {
            Color.accentColor.opacity(0.14)
        } else if account.id == hoveredAccountID {
            Color.primary.opacity(0.055)
        } else {
            Color.clear
        }
        return RoundedRectangle(cornerRadius: 9, style: .continuous)
            .fill(color)
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

    @ViewBuilder
    private var confirmationButtons: some View {
        if let confirmation {
            switch confirmation {
            case let .switchAccount(account):
                Button("Switch Account") {
                    self.confirmation = nil
                    Task { await model.switchAccount(to: account.id) }
                }
                .disabled(model.isMutating)
            case let .bankedReset(account):
                Button("Use Reset") {
                    self.confirmation = nil
                    Task { await model.redeemBankedReset(for: account.id) }
                }
                .disabled(model.isMutating)
            case let .remove(account):
                Button("Remove", role: .destructive) {
                    self.confirmation = nil
                    Task { await model.removeAccount(id: account.id) }
                }
                .disabled(model.isMutating)
            }
            Button("Cancel", role: .cancel) { self.confirmation = nil }
        }
    }
}

private extension View {
    func islandNonSelectableText() -> some View {
        textSelection(.disabled)
    }
}

private enum AddAccountFlow: Equatable {
    case chooseProvider
    case pasteSession(AccountKind)
}

private extension AccountKind {
    var displayName: String {
        switch self {
        case .codex: "Codex"
        case .cursor: "Cursor"
        case .claude: "Claude"
        }
    }

    var sessionPasteInstructions: String {
        switch self {
        case .codex:
            ""
        case .cursor:
            "After signing in to Cursor in your browser, paste the Cookie header for cursor.com (from the network tab or a copy-cookies extension)."
        case .claude:
            "After signing in to Claude in your browser, paste the sessionKey value or full Cookie header for claude.ai."
        }
    }
}

private enum AccountConfirmation {
    case switchAccount(AccountProfile)
    case bankedReset(AccountProfile)
    case remove(AccountProfile)

    var title: String {
        switch self {
        case let .switchAccount(account): "Switch to \(account.displayName)?"
        case let .bankedReset(account): "Use a banked reset for \(account.displayName)?"
        case let .remove(account): "Remove \(account.displayName)?"
        }
    }

    @MainActor
    func message(model: AppModel) -> String {
        switch self {
        case .switchAccount:
            return "The active credential file will be swapped immediately. Codex Desktop is not restarted. Quit and reopen it manually to use the new account. Existing CLI sessions stay open; new CLI sessions use the selected account."
        case let .bankedReset(account):
            var message = "This uses one saved reset credit for \(account.displayName). It immediately refreshes eligible Codex usage windows and may move the next weekly reset date. This cannot be undone. No payment is made."
            if let usage = model.usageStates[account.id]?.displayedUsage,
               let credits = usage.availableResetCredits {
                message += "\n\nCredit expiries (earliest listed will be used):\n\(resetExpirySummary(credits: credits, availableCount: usage.availableResetCount))"
            }
            return message
        case .remove:
            return "This removes only the saved local profile from this Mac."
        }
    }
}

private func resetExpiryHelp(usage: WeeklyUsage) -> String {
    guard let credits = usage.availableResetCredits else {
        return "Redeem one saved usage reset"
    }
    return """
        Use a banked reset
        Credit expiries (earliest listed used):
        \(resetExpirySummary(credits: credits, availableCount: usage.availableResetCount))
        """
}

private func resetExpirySummary(
    credits: [BankedResetCredit],
    availableCount: Int?
) -> String {
    guard !credits.isEmpty else { return "Expiry details unavailable" }
    var expiries = credits.map { credit in
        credit.expiresAt?.formatted(.dateTime.month(.abbreviated).day().year().hour().minute())
            ?? "Does not expire"
    }
    let additionalCount = max(0, (availableCount ?? credits.count) - credits.count)
    if additionalCount > 0 {
        expiries.append("\(additionalCount) additional credits not listed")
    }
    return expiries.joined(separator: "\n")
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
