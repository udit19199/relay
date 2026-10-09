import SidekickCore
import SwiftUI

struct AccountRow: View {
    let account: AccountProfile
    let usageState: UsageViewState
    let now: Date

    private var accountTitle: String {
        account.email.flatMap { $0.isEmpty ? nil : $0 } ?? account.displayName
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline, spacing: 7) {
                Text(verbatim: accountTitle)
                    .font(.system(size: 14, weight: .semibold))
                    .lineLimit(1)
                    .truncationMode(.tail)
                    .help(accountTitle)
                Spacer(minLength: 4)
                if let message = usageState.refreshError {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .font(.system(size: 9))
                        .foregroundStyle(.orange)
                        .help(message)
                        .accessibilityLabel(message)
                }
            }

            usageContent
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 9)
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

    private func expandedUsageContent(_ usage: WeeklyUsage) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            if let remaining = usage.fiveHourRemainingPercent,
               let resetsAt = usage.fiveHourResetsAt {
                limitRow(
                    title: "5-hour",
                    remainingPercent: remaining,
                    resetsAt: resetsAt,
                    includesDate: false
                )
            }
            limitRow(
                title: "Weekly",
                remainingPercent: usage.remainingPercent,
                resetsAt: usage.resetsAt,
                includesDate: true
            )
        }
    }

    private func limitRow(
        title: String,
        remainingPercent: Int,
        resetsAt: Date,
        includesDate: Bool
    ) -> some View {
        HStack(spacing: 8) {
            VStack(alignment: .leading, spacing: 1) {
                Text(title)
                    .font(.system(size: 12, weight: .medium))
                    .foregroundStyle(.secondary)

                Text("\(remainingPercent)% left")
                    .font(.system(size: 22, weight: .semibold).monospacedDigit())
                    .lineLimit(1)
                    .fixedSize(horizontal: true, vertical: false)
            }

            Spacer(minLength: 4)

            VStack(alignment: .trailing, spacing: 2) {
                Text(ResetCountdown.text(until: resetsAt, now: now))
                    .font(.system(size: 12, weight: .medium).monospacedDigit())

                Text(ResetCountdown.timestampText(until: resetsAt, includesDate: includesDate))
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
                    .help(ResetCountdown.absoluteText(until: resetsAt, includesDate: includesDate))
            }
            .lineLimit(1)
            .minimumScaleFactor(0.85)
        }
    }
}

/// Codex-bar style reset countdowns: how long until this window comes back.
enum ResetCountdown {
    static func text(until resetsAt: Date, now: Date) -> String {
        guard resetsAt > now else { return "Resets now" }
        return "Resets in \(duration(resetsAt.timeIntervalSince(now)))"
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
