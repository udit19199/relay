// SPDX-License-Identifier: MIT
import RelayCore
import AppKit
import SwiftUI

// Relay's accounts and usage, inside the notch island. Relay serves every
// account at once: rows are display-only. Adding signs in to Claude through
// Relay's browser OAuth or saves an OpenCode Go key; removing deletes the credential.
struct IslandAccountManager: View {
    @ObservedObject var model: AppModel
    @Binding var showsCompactUsage: Bool
    /// True while the island must stay open (a dialog, or typing a key).
    let onHoldOpenChanged: (Bool) -> Void
    /// Asks the panel for keyboard focus so a text field can take input.
    let onNeedsKeyboard: () -> Void
    let onContentHeightChanged: (CGFloat) -> Void

    @State private var removingAccount: RelayAccount?
    @State private var flow: AddFlow?
    @State private var apiKey = ""
    @State private var keyError: String?
    @State private var isSavingKey = false
    // Measured heights that size the island to its content.
    @State private var bannerHeight: CGFloat = 0
    @State private var bodyHeight: CGFloat = 0
    @State private var footerHeight: CGFloat = 0

    var body: some View {
        VStack(spacing: 0) {
            if let error = model.visibleError {
                VStack(spacing: 0) {
                    InlineErrorBanner(title: error.title, message: error.message, onDismiss: model.dismissError)
                    Divider().padding(.horizontal, 10)
                }
                .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { bannerHeight = $0 }
            }

            Group {
                if let flow {
                    addPanel(flow)
                        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { bodyHeight = $0 }
                } else {
                    ScrollView {
                        accountList
                            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { bodyHeight = $0 }
                    }
                    .scrollIndicators(.hidden)
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)

            footer
                .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { footerHeight = $0 }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        .onChange(of: model.visibleError == nil) { _, isHidden in
            if isHidden { bannerHeight = 0 }
        }
        .onChange(of: [bannerHeight, bodyHeight, footerHeight]) { _, _ in
            // Keep the last size until the body and footer have been measured.
            guard bodyHeight > 0, footerHeight > 0 else { return }
            onContentHeightChanged(bannerHeight + bodyHeight + footerHeight)
        }
        .onChange(of: model.isAddingAccount) { _, isAdding in
            if !isAdding, flow == .claudeWaiting { flow = nil }
        }
        .onChange(of: removingAccount != nil || flow == .openCodeKey) { _, hold in
            onHoldOpenChanged(hold)
        }
        .confirmationDialog(
            removingAccount.map { "Remove \($0.email ?? $0.providerName)?" } ?? "Remove account",
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
            }
            Button("Cancel", role: .cancel) { removingAccount = nil }
        } message: {
            Text("This deletes the credential from Relay. Requests stop using this account.")
        }
    }

    // MARK: - Accounts

    private var accountList: some View {
        Group {
            if model.accounts.isEmpty {
                VStack(spacing: 4) {
                    Text("No accounts yet")
                        .font(.system(size: 12.5, weight: .semibold))
                    Text("Add a Claude or OpenCode Go account to see its limits here.")
                        .font(.system(size: 11))
                        .foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, 24)
            } else {
                TimelineView(.periodic(from: .now, by: 30)) { context in
                    VStack(spacing: 2) {
                        UsageTableHeader()
                            .padding(.bottom, 2)
                        if claudeAccounts.count > 1 {
                            PoolTableRow(pool: model.pooledUsage(at: context.date), now: context.date)
                                .padding(.bottom, 2)
                        }
                        ForEach(claudeAccounts) { accountRow($0, now: context.date) }
                        // Other providers are tracked on their own, below the Claude group.
                        if !otherAccounts.isEmpty {
                            Divider()
                                .padding(.horizontal, 10)
                                .padding(.vertical, 4)
                            ForEach(otherAccounts) { accountRow($0, now: context.date) }
                        }
                    }
                }
                .padding(.vertical, 8)
            }
        }
    }

    private var claudeAccounts: [RelayAccount] {
        model.accounts.filter { $0.provider == RelayProvider.claude.rawValue }
    }

    private var otherAccounts: [RelayAccount] {
        model.accounts.filter { $0.provider != RelayProvider.claude.rawValue }
    }

    private func accountRow(_ account: RelayAccount, now: Date) -> some View {
        AccountRow(
            account: account,
            usageState: model.usageStates[account.id] ?? .idle,
            now: now,
            onRemove: { removingAccount = account }
        )
        .contextMenu {
            Button("Remove Account…", role: .destructive) { removingAccount = account }
        }
    }

    // MARK: - Add account

    @ViewBuilder
    private func addPanel(_ flow: AddFlow) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            switch flow {
            case .chooseProvider:
                panelTitle("Add an account", detail: "Relay uses every account at once.")
                VStack(spacing: 6) {
                    ProviderOption(provider: .claude, detail: "Sign in with your browser") {
                        startClaudeSignIn()
                    }
                    ProviderOption(provider: .openCodeGo, detail: "Paste an API key") {
                        apiKey = ""
                        keyError = nil
                        self.flow = .openCodeKey
                        onNeedsKeyboard()
                    }
                }
                Button("Cancel") { self.flow = nil }
                    .buttonStyle(IslandButtonStyle())
                    .keyboardShortcut(.cancelAction)

            case .claudeWaiting:
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    ProgressView().controlSize(.small)
                    panelTitle("Finish signing in to Claude", detail: "Complete it in your browser. The account appears here when Relay saves it.")
                }
                Button("Cancel Sign-In") { cancelClaudeSignIn() }
                    .buttonStyle(IslandButtonStyle())
                    .keyboardShortcut(.cancelAction)

            case .openCodeKey:
                panelTitle("Add OpenCode Go", detail: "Relay checks the key with OpenCode Go before saving it.")
                SecureField("API key", text: $apiKey)
                    .textFieldStyle(.plain)
                    .font(.system(size: 12.5).monospaced())
                    .padding(.horizontal, 10)
                    .padding(.vertical, 7)
                    .background(Color.primary.opacity(0.07), in: RoundedRectangle(cornerRadius: 7, style: .continuous))
                    .onSubmit(saveKey)
                    .disabled(isSavingKey)
                if let keyError {
                    Text(keyError)
                        .font(.system(size: 11))
                        .foregroundStyle(Color(red: 0.95, green: 0.55, blue: 0.45))
                }
                HStack(spacing: 8) {
                    Spacer()
                    Button("Cancel") { self.flow = nil }
                        .buttonStyle(IslandButtonStyle())
                        .keyboardShortcut(.cancelAction)
                    Button(action: saveKey) {
                        HStack(spacing: 6) {
                            if isSavingKey { ProgressView().controlSize(.mini) }
                            Text(isSavingKey ? "Checking…" : "Add Account")
                        }
                    }
                    .buttonStyle(IslandButtonStyle(prominent: true))
                    .disabled(isSavingKey || apiKey.trimmingCharacters(in: .whitespaces).isEmpty)
                }
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 12)
    }

    private func panelTitle(_ title: String, detail: String) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(title).font(.system(size: 12.5, weight: .semibold))
            Text(detail)
                .font(.system(size: 11))
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
        .textSelection(.disabled)
    }

    private func startClaudeSignIn() {
        Task {
            guard let url = await model.beginClaudeSignIn() else { return }
            NSWorkspace.shared.open(url)
            flow = .claudeWaiting
        }
    }

    private func cancelClaudeSignIn() {
        model.cancelAddingAccount()
        flow = nil
    }

    private func saveKey() {
        guard !isSavingKey else { return }
        isSavingKey = true
        keyError = nil
        Task {
            let error = await model.addOpenCodeGoAccount(apiKey: apiKey)
            isSavingKey = false
            if let error {
                keyError = error
            } else {
                apiKey = ""
                flow = nil
            }
        }
    }

    // MARK: - Footer

    private var footer: some View {
        VStack(spacing: 0) {
            Divider().padding(.horizontal, 10)
            HStack(spacing: 10) {
                if flow == nil {
                    if model.isAddingAccount {
                        Button(action: cancelClaudeSignIn) {
                            HStack(spacing: 6) {
                                ProgressView().controlSize(.mini)
                                Text("Cancel Claude Sign-In")
                            }
                        }
                        .buttonStyle(IslandButtonStyle())
                    } else {
                        Button { flow = .chooseProvider } label: {
                            Label("Add Account", systemImage: "plus")
                        }
                        .buttonStyle(IslandButtonStyle(prominent: true))
                        .disabled(model.isMutating)
                    }
                }
                Spacer(minLength: 8)
                Toggle(isOn: $showsCompactUsage) {
                    Text("Beside notch")
                        .font(.system(size: 11, weight: .medium))
                        .foregroundStyle(.secondary)
                }
                .toggleStyle(.switch)
                .controlSize(.mini)
                .help("Show the Claude totals beside the notch while Relay is closed")
                Button { NSApp.terminate(nil) } label: {
                    Image(systemName: "power")
                        .font(.system(size: 11, weight: .semibold))
                        .frame(width: 14, height: 14)
                }
                .buttonStyle(IslandButtonStyle(compact: true))
                .keyboardShortcut("q", modifiers: .command)
                .help("Quit Relay (⌘Q)")
                .accessibilityLabel("Quit Relay")
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 10)
        }
    }
}

private enum AddFlow: Equatable {
    case chooseProvider
    case claudeWaiting
    case openCodeKey
}

/// One choice in the add-account panel: provider colour, name, and how it signs in.
private struct ProviderOption: View {
    let provider: RelayProvider
    let detail: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 8) {
                ProviderDot(provider: provider.rawValue)
                Text(provider.displayName).font(.system(size: 12.5, weight: .medium))
                Text(detail).font(.system(size: 11)).foregroundStyle(.secondary)
                Spacer(minLength: 4)
                Image(systemName: "chevron.right")
                    .font(.system(size: 10, weight: .semibold))
                    .foregroundStyle(.tertiary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .buttonStyle(IslandButtonStyle(fillsWidth: true))
    }
}

/// Shared island button: quiet at rest, visible hover and press states,
/// dimmed when disabled. `prominent` marks the primary action.
struct IslandButtonStyle: ButtonStyle {
    var prominent = false
    var compact = false
    var fillsWidth = false

    func makeBody(configuration: Configuration) -> some View {
        StyledButton(configuration: configuration, prominent: prominent, compact: compact, fillsWidth: fillsWidth)
    }

    private struct StyledButton: View {
        let configuration: ButtonStyleConfiguration
        let prominent: Bool
        let compact: Bool
        let fillsWidth: Bool
        @Environment(\.isEnabled) private var isEnabled
        @State private var isHovering = false

        var body: some View {
            configuration.label
                .font(.system(size: 12, weight: .medium))
                .lineLimit(1)
                .foregroundStyle(prominent ? Color.primary : Color.secondary)
                .padding(.horizontal, compact ? 6 : 10)
                .padding(.vertical, compact ? 6 : 6)
                .frame(maxWidth: fillsWidth ? .infinity : nil, alignment: .leading)
                .background(background, in: RoundedRectangle(cornerRadius: 7, style: .continuous))
                .contentShape(RoundedRectangle(cornerRadius: 7, style: .continuous))
                .opacity(isEnabled ? 1 : 0.4)
                .onHover { isHovering = $0 && isEnabled }
                .animation(.easeOut(duration: 0.12), value: isHovering)
        }

        private var background: Color {
            let base = prominent || fillsWidth ? 0.08 : 0
            if configuration.isPressed { return Color.primary.opacity(base + 0.12) }
            if isHovering { return Color.primary.opacity(base + 0.06) }
            return Color.primary.opacity(base)
        }
    }
}

private struct InlineErrorBanner: View {
    let title: String
    let message: String
    let onDismiss: () -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Image(systemName: "exclamationmark.triangle.fill")
                .font(.system(size: 11))
                .foregroundStyle(.orange)
                .padding(.top, 2)
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.system(size: 11.5, weight: .semibold))
                Text(message)
                    .font(.system(size: 10.5))
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 4)
            Button(action: onDismiss) {
                Image(systemName: "xmark").font(.system(size: 9, weight: .semibold))
            }
            .buttonStyle(IslandButtonStyle(compact: true))
            .help("Dismiss")
            .accessibilityLabel("Dismiss")
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 8)
    }
}
