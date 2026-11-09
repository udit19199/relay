// SPDX-License-Identifier: MIT
import RelayCore
import SwiftUI

// Compact usage table: one line per account, a 5-hour and a weekly column,
// each showing percent left and time until that window resets.
enum UsageTable {
    static let windowWidth: CGFloat = 112
    static let spacing: CGFloat = 10
}

struct UsageTableHeader: View {
    var body: some View {
        HStack(spacing: UsageTable.spacing) {
            Spacer(minLength: 0)
            label("5-HOUR")
            label("WEEKLY")
        }
        .padding(.horizontal, 10)
    }

    private func label(_ text: String) -> some View {
        Text(text)
            .font(.system(size: 9, weight: .semibold))
            .foregroundStyle(.secondary)
            .frame(width: UsageTable.windowWidth, alignment: .leading)
    }
}

/// Total across every account, the first line of the table.
struct PoolTableRow: View {
    let pool: PooledUsage
    let now: Date

    var body: some View {
        HStack(spacing: UsageTable.spacing) {
            Text("All accounts")
                .font(.system(size: 12.5, weight: .semibold))
                .lineLimit(1)
                .frame(maxWidth: .infinity, alignment: .leading)
            cell(pool.fiveHour, includesDate: false)
            cell(pool.weekly, includesDate: true)
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 8)
        .background(Color.primary.opacity(0.06), in: RoundedRectangle(cornerRadius: 8, style: .continuous))
    }

    private func cell(_ window: PooledWindow?, includesDate: Bool) -> some View {
        guard let window else { return UsageCell(percent: nil, detail: nil, help: "Unavailable") }
        if let next = window.nextResetAt, window.nextResetGainPercent > 0 {
            return UsageCell(
                percent: window.remainingPercent,
                detail: "+\(window.nextResetGainPercent)% \(ResetCountdown.short(until: next, now: now))",
                help: "+\(window.nextResetGainPercent)% comes back \(ResetCountdown.timestampText(until: next, includesDate: includesDate))"
            )
        }
        return UsageCell(percent: window.remainingPercent, detail: nil, help: "No refill pending")
    }
}

struct AccountRow: View {
    let account: RelayAccount
    let usageState: UsageViewState
    let now: Date

    private var title: String {
        guard let email = account.email, let at = email.firstIndex(of: "@") else { return account.displayName }
        return String(email[..<at])
    }

    // Disabled, rate limited, or a failed refresh: one icon, details on hover.
    private var warning: String? {
        if account.disabled || account.unavailable {
            return account.statusSummary
        }
        return usageState.refreshError
    }

    var body: some View {
        HStack(spacing: UsageTable.spacing) {
            HStack(spacing: 5) {
                Text(verbatim: title)
                    .font(.system(size: 12.5, weight: .medium))
                    .lineLimit(1)
                    .truncationMode(.middle)
                if let warning {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .font(.system(size: 9))
                        .foregroundStyle(.orange)
                        .help(warning)
                        .accessibilityLabel(warning)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .help("\(account.email ?? account.displayName) · \(account.provider.capitalized)")

            usageCells
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
        .contentShape(Rectangle())
    }

    @ViewBuilder
    private var usageCells: some View {
        if let stored = usageState.displayedUsage {
            let usage = stored.current(at: now)
            if let percent = usage.fiveHourRemainingPercent, let resetsAt = usage.fiveHourResetsAt {
                cell(percent, resetsAt, includesDate: false)
            } else {
                UsageCell(percent: nil, detail: nil, help: "No 5-hour window")
            }
            cell(usage.remainingPercent, usage.resetsAt, includesDate: true)
        } else {
            let help = if case let .unavailable(message) = usageState { message } else { "Usage is loading" }
            UsageCell(percent: nil, detail: nil, help: help)
            UsageCell(percent: nil, detail: nil, help: help)
        }
    }

    private func cell(_ percent: Int, _ resetsAt: Date, includesDate: Bool) -> UsageCell {
        UsageCell(
            percent: percent,
            detail: ResetCountdown.short(until: resetsAt, now: now),
            help: ResetCountdown.absoluteText(until: resetsAt, includesDate: includesDate)
        )
    }
}

/// Percent left in its usage colour, then a small detail such as the reset time.
struct UsageCell: View {
    let percent: Int?
    let detail: String?
    let help: String

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            Text(percent.map { "\($0)%" } ?? "—")
                .font(.system(size: 12.5, weight: .semibold).monospacedDigit())
                .foregroundStyle(UsageTint.color(for: percent))
                .frame(width: 36, alignment: .leading)
            if let detail {
                Text(detail)
                    .font(.system(size: 10.5, weight: .medium).monospacedDigit())
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .minimumScaleFactor(0.85)
            }
        }
        .frame(width: UsageTable.windowWidth, alignment: .leading)
        .help(help)
    }
}

/// Codex-bar style reset countdowns: how long until this window comes back.
enum ResetCountdown {
    static func text(until resetsAt: Date, now: Date) -> String {
        guard resetsAt > now else { return "Refilled" }
        return "Resets \(relative(until: resetsAt, now: now))"
    }

    /// "1h 20m", or "now" once the time has passed.
    static func short(until date: Date, now: Date) -> String {
        guard date > now else { return "now" }
        return duration(date.timeIntervalSince(now))
    }

    /// "in 1h 20m", or "now" once the time has passed.
    static func relative(until date: Date, now: Date) -> String {
        guard date > now else { return "now" }
        return "in \(duration(date.timeIntervalSince(now)))"
    }

    /// The exact local reset date and time.
    static func timestampText(until resetsAt: Date, includesDate: Bool) -> String {
        let date = includesDate
            ? resetsAt.formatted(.dateTime.weekday(.abbreviated).month(.abbreviated).day().hour().minute())
            : resetsAt.formatted(.dateTime.month(.abbreviated).day().hour().minute())
        return date
    }

    static func absoluteText(until resetsAt: Date, includesDate: Bool) -> String {
        "Resets \(timestampText(until: resetsAt, includesDate: includesDate))"
    }

    private static func duration(_ interval: TimeInterval) -> String {
        let totalMinutes = Int(interval / 60)
        if totalMinutes < 1 { return "a moment" }
        if totalMinutes < 60 { return "\(totalMinutes)m" }
        let hours = totalMinutes / 60
        if hours < 24 { return "\(hours)h \(totalMinutes % 60)m" }
        let days = hours / 24
        let remainderHours = hours % 24
        return remainderHours == 0 ? "\(days)d" : "\(days)d \(remainderHours)h"
    }
}

enum UsageTint {
    static func color(for remainingPercent: Int?) -> Color {
        guard let remainingPercent else { return .secondary }
        if remainingPercent <= 30 { return Color(red: 0.91, green: 0.43, blue: 0.42) }
        if remainingPercent <= 70 { return Color(red: 0.91, green: 0.72, blue: 0.34) }
        return Color(red: 0.43, green: 0.81, blue: 0.55)
    }
}
