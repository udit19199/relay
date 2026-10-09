import SidekickCore
import AppKit
import SwiftUI

struct MenuBarPopover: View {
    @ObservedObject var model: AppModel
    @State private var page: PopoverPage = .accounts
    @State private var hoveredAccountID: UUID?

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
                switch page {
                case .accounts:
                    accountPage
                case let .confirmSwitch(account):
                    SwitchConfirmationPage(
                        model: model,
                        account: account,
                        onCancel: {
                            page = .accounts
                        },
                        onConfirm: {
                            page = .accounts
                            Task { await model.switchAccount(to: account.id) }
                        }
                    )
                    .disabled(model.isMutating)
                case let .confirmReset(account):
                    ResetConfirmationPage(
                        model: model,
                        account: account,
                        onCancel: {
                            page = .accounts
                        },
                        onConfirm: {
                            page = .accounts
                            Task { await model.redeemBankedReset(for: account.id) }
                        }
                    )
                    .disabled(model.isMutating)
                case let .remove(account):
                    RemoveConfirmationPage(
                        model: model,
                        account: account,
                        onCancel: {
                            page = .accounts
                        },
                        onConfirm: {
                            page = .accounts
                            Task { await model.removeAccount(id: account.id) }
                        }
                    )
                    .disabled(model.isMutating)
                }
            }
        }
        .frame(width: 360)
        .onAppear {
            page = .accounts
        }
        .task {
            await model.start()
            model.refreshWeeklyUsage()
        }
    }

    private var accountPage: some View {
        VStack(spacing: 0) {
            if model.accounts.isEmpty {
                Text("No saved accounts")
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 28)
            } else {
                VStack(spacing: 2) {
                    ForEach(model.accounts) { account in
                        HStack(spacing: 4) {
                            Button {
                                if account.id == model.activeAccountID {
                                    NSApp.keyWindow?.close()
                                } else {
                                    page = .confirmSwitch(account)
                                }
                            } label: {
                                TimelineView(.periodic(from: .now, by: 30)) { context in
                                    AccountRow(
                                        account: account,
                                        usageState: model.usageStates[account.id] ?? .idle,
                                        now: context.date
                                    )
                                }
                            }
                            .buttonStyle(.plain)
                            .frame(maxWidth: .infinity)
                            .disabled(model.isMutating)

                            if let usage = model.usageStates[account.id]?.displayedUsage,
                               let count = usage.availableResetCount,
                               count > 0 {
                                Button {
                                    page = .confirmReset(account)
                                } label: {
                                    HStack(spacing: 6) {
                                        Image(systemName: "arrow.counterclockwise")
                                        Text("\(count)")
                                            .monospacedDigit()
                                    }
                                    .font(.system(size: 12, weight: .semibold))
                                    .foregroundStyle(.secondary)
                                    .padding(.horizontal, 8)
                                    .frame(minWidth: 44, minHeight: 32)
                                    .background(Color.primary.opacity(0.06), in: Capsule())
                                }
                                .buttonStyle(.plain)
                                .accessibilityLabel("Use a banked usage reset for \(account.displayName)")
                                .accessibilityValue("\(count) available")
                                .help(resetExpiryHelp(usage: usage))
                                .disabled(model.isMutating)
                            }
                        }
                        .padding(.trailing, 8)
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
                                    page = .remove(account)
                                } label: {
                                    Text("Remove \(account.displayName)?")
                                }
                            }
                        }
                    }
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 8)
            }

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
}

private enum PopoverPage {
    case accounts
    case confirmSwitch(AccountProfile)
    case confirmReset(AccountProfile)
    case remove(AccountProfile)
}

private struct SwitchConfirmationPage: View {
    @ObservedObject var model: AppModel
    let account: AccountProfile
    let onCancel: () -> Void
    let onConfirm: () -> Void

    var body: some View {
        VStack(spacing: 0) {
            PopoverHeader(
                title: "Switch to \(account.displayName)?",
                backTitle: "Cancel",
                onBack: onCancel
            )

            Divider()

            VStack(alignment: .leading, spacing: 14) {
                Text("The active credential file will be swapped immediately. Codex Desktop is not restarted — quit and reopen it manually to use the new account. Existing CLI sessions stay open; new CLI sessions use the selected account.")
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)

                HStack(spacing: 7) {
                    Button("Cancel", action: onCancel)
                        .buttonStyle(.bordered)
                        .frame(maxWidth: .infinity)

                    Button("Switch Account", action: onConfirm)
                        .buttonStyle(.borderedProminent)
                        .frame(maxWidth: .infinity)
                }
            }
            .padding(14)
        }
    }
}

private struct ResetConfirmationPage: View {
    @ObservedObject var model: AppModel
    let account: AccountProfile
    let onCancel: () -> Void
    let onConfirm: () -> Void

    var body: some View {
        VStack(spacing: 0) {
            PopoverHeader(
                title: "Use a banked reset for \(account.displayName)?",
                backTitle: "Cancel",
                onBack: onCancel
            )

            Divider()

            VStack(alignment: .leading, spacing: 14) {
                Text("This uses one saved reset credit for \(account.displayName). It immediately refreshes eligible Codex usage windows and may move the next weekly reset date. This cannot be undone. No payment is made.")
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)

                if let usage = model.usageStates[account.id]?.displayedUsage,
                   let credits = usage.availableResetCredits {
                    Text(
                        "Credit expiries (earliest listed will be used):\n\(resetExpirySummary(credits: credits, availableCount: usage.availableResetCount))"
                    )
                    .font(.system(size: 10.5))
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                }

                HStack(spacing: 7) {
                    Button("Cancel", action: onCancel)
                        .buttonStyle(.bordered)
                        .frame(maxWidth: .infinity)

                    Button("Use Reset", action: onConfirm)
                        .buttonStyle(.borderedProminent)
                        .frame(maxWidth: .infinity)
                        .disabled(model.isMutating)
                }
            }
            .padding(14)
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

private struct RemoveConfirmationPage: View {
    @ObservedObject var model: AppModel
    let account: AccountProfile
    let onCancel: () -> Void
    let onConfirm: () -> Void

    var body: some View {
        VStack(spacing: 0) {
            PopoverHeader(
                title: "Remove \(account.displayName)?",
                backTitle: "Cancel",
                onBack: onCancel
            )

            Divider()

            VStack(alignment: .leading, spacing: 14) {
                Text("This removes only the saved local profile from this Mac.")
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)

                HStack(spacing: 7) {
                    Button("Cancel", action: onCancel)
                        .buttonStyle(.bordered)
                        .frame(maxWidth: .infinity)

                    Button("Remove", role: .destructive, action: onConfirm)
                        .buttonStyle(.borderedProminent)
                        .tint(.red)
                        .frame(maxWidth: .infinity)
                        .disabled(model.isMutating)
                }
            }
            .padding(14)
        }
    }
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

private struct PopoverHeader: View {
    let title: String
    let backTitle: String
    let onBack: () -> Void

    var body: some View {
        HStack(spacing: 7) {
            Button(action: onBack) {
                Image(systemName: "chevron.left")
                    .font(.system(size: 11, weight: .semibold))
                    .frame(width: 27, height: 27)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel(backTitle)
            .help(backTitle)

            Text(title)
                .font(.system(size: 13, weight: .semibold))
                .fixedSize(horizontal: false, vertical: true)
            Spacer()
        }
        .padding(.horizontal, 7)
        .padding(.vertical, 5)
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
