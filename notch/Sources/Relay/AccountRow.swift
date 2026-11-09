// SPDX-License-Identifier: MIT
import RelayCore
import SwiftUI

struct AccountRow: View {
    let account: RelayAccount
    let usageState: UsageViewState
    let now: Date

    private var accountTitle: String {
        account.email ?? account.displayName
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            HStack(alignment: .firstTextBaseline, spacing: 7) {
                Text(verbatim: accountTitle)
                    .font(.system(size: 13, weight: .semibold))
                    .lineLimit(1)
                    .truncationMode(.tail)
                    .help(accountTitle)
                Text(verbatim: account.provider.uppercased())
                    .font(.system(size: 9, weight: .medium))
                    .foregroundStyle(.secondary)
                    .accessibilityLabel("\(account.provider) account")
                Spacer(minLength: 4)
                if let message = usageState.refreshError {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .font(.system(size: 9))
                        .foregroundStyle(.orange)
                        .help(message)
                        .accessibilityLabel(message)
                }
            }
            .frame(height: 28)

            usageContent
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 12)
        .contentShape(Rectangle())
    }

    @ViewBuilder
    private var usageContent: some View {
        switch usageState {
        case .idle:
            Text("Usage is loading")
                .font(.system(size: 13))
                .foregroundStyle(.secondary)
        case let .unavailable(message):
            Text("Usage unavailable")
                .font(.system(size: 13))
                .foregroundStyle(.secondary)
                .help(message)
        case let .loaded(usage), let .stale(usage, _):
            expandedUsageContent(usage)
        }
    }

    private func expandedUsageContent(_ stored: WeeklyUsage) -> some View {
        let usage = stored.current(at: now)
        return VStack(alignment: .leading, spacing: 6) {
            if let remaining = usage.fiveHourRemainingPercent,
               let resetsAt = usage.fiveHourResetsAt {
                LimitRow(
                    title: "5-hour",
                    remainingPercent: remaining,
                    detail: ResetCountdown.text(until: resetsAt, now: now),
                    help: ResetCountdown.absoluteText(until: resetsAt, includesDate: false)
                )
            }
            LimitRow(
                title: "Weekly",
                remainingPercent: usage.remainingPercent,
                detail: ResetCountdown.text(until: usage.resetsAt, now: now),
                help: ResetCountdown.absoluteText(until: usage.resetsAt, includesDate: true)
            )
        }
    }
}

/// Total capacity across every account, shown above the account rows.
struct PoolSummaryRow: View {
    let pool: PooledUsage
    let accountCount: Int
    let now: Date

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            HStack(alignment: .firstTextBaseline, spacing: 7) {
                Text("All accounts")
                    .font(.system(size: 13, weight: .semibold))
                Text("\(accountCount) POOLED")
                    .font(.system(size: 9, weight: .medium))
                    .foregroundStyle(.secondary)
                Spacer(minLength: 4)
            }
            .frame(height: 28)

            VStack(alignment: .leading, spacing: 6) {
                if let window = pool.fiveHour {
                    row("5-hour", window, includesDate: false)
                }
                if let window = pool.weekly {
                    row("Weekly", window, includesDate: true)
                }
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 12)
        .background(
            Color.primary.opacity(0.05),
            in: RoundedRectangle(cornerRadius: 9, style: .continuous)
        )
    }

    private func row(_ title: String, _ window: PooledWindow, includesDate: Bool) -> some View {
        let detail: String
        let help: String
        if let next = window.nextResetAt, window.nextResetGainPercent > 0 {
            detail = "+\(window.nextResetGainPercent)% \(ResetCountdown.relative(until: next, now: now))"
            help = "Next refill \(ResetCountdown.timestampText(until: next, includesDate: includesDate))"
        } else {
            detail = window.remainingPercent >= 100 ? "Full" : "No refill pending"
            help = detail
        }
        return LimitRow(title: title, remainingPercent: window.remainingPercent, detail: detail, help: help)
    }
}

/// One limit line: window name, percent left, and when it changes.
struct LimitRow: View {
    let title: String
    let remainingPercent: Int
    let detail: String
    let help: String

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(title)
                .font(.system(size: 11, weight: .medium))
                .foregroundStyle(.secondary)
                .frame(width: 48, alignment: .leading)

            Text("\(remainingPercent)% left")
                .font(.system(size: 12, weight: .medium).monospacedDigit())
                .foregroundStyle(UsageTint.color(for: remainingPercent))
                .lineLimit(1)
                .frame(width: 72, alignment: .leading)

            Spacer(minLength: 4)

            Text(detail)
                .font(.system(size: 11, weight: .medium).monospacedDigit())
                .lineLimit(1)
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, alignment: .trailing)
                .minimumScaleFactor(0.8)
                .help(help)
        }
    }
}

/// Codex-bar style reset countdowns: how long until this window comes back.
enum ResetCountdown {
    static func text(until resetsAt: Date, now: Date) -> String {
        guard resetsAt > now else { return "Refilled" }
        return "Resets \(relative(until: resetsAt, now: now))"
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
