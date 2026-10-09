import SidekickCore
import AppKit
import SwiftUI

struct IslandAccountManager: View {
    @ObservedObject var model: AppModel
    let onDismiss: () -> Void
    var onConfirmationChanged: (Bool) -> Void = { _ in }
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var confirmation: AccountConfirmation?
    @State private var hoveredAccountID: UUID?
    @State private var hoveredResetAccountID: UUID?

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

            ScrollView { accountList }
                .scrollIndicators(.hidden)

            Divider()
                .padding(.horizontal, 16)

            HStack(spacing: 2) {
                accountActions
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
                                if account.id == model.activeAccountID {
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
                                        hasResetControl: model.usageStates[account.id]?.displayedUsage?.availableResetCount != nil
                                    )
                                }
                            }
                            .buttonStyle(.plain)
                            .frame(maxWidth: .infinity)
                            .disabled(model.isMutating)

                            if let usage = model.usageStates[account.id]?.displayedUsage,
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
            title: model.isAddingAccount ? "Cancel Sign-In" : "Add Account",
            systemImage: model.isAddingAccount ? "xmark" : "plus",
            showsProgress: model.isAddingAccount
        ) {
            if model.isAddingAccount {
                model.cancelAddingAccount()
            } else {
                model.addAccount()
            }
        }
        .disabled(model.isMutating && !model.isAddingAccount)
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
