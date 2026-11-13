import Foundation

/// The usage lines of the menu bar menu: the tokens of the day, then each AI's own usage windows.
public enum UsageLines {
    /// The names of the AIs, kept as they are in every language.
    private static let names = ["codex": "Codex", "claude": "Claude"]

    /// A window that started again since it was reported is left out: the snapshot may be older than the reset.
    public static func lines(_ usage: AssistantUsage?, language: Language? = nil, now: Date = Date()) -> [String] {
        guard let usage else { return [] }
        let language = language ?? L10n.current
        var lines: [String] = []
        if let tokens = usage.tokensToday, tokens.total > 0 {
            lines.append(L10n.text(.usageTokensToday, compact(tokens.total, language), language: language))
        }
        let providers = (usage.limits ?? [:]).map { (name: names[$0.key] ?? $0.key, limits: $0.value) }.sorted { $0.name < $1.name }
        for provider in providers {
            var parts = [provider.name]
            let holds = { (window: UsageWindow) in window.resetsAt.map { $0 > now } ?? true }
            if let window = provider.limits.fiveHour, holds(window) { parts.append(L10n.text(.usageFiveHour, percent(window.usedPercent, language), language: language)) }
            if let window = provider.limits.week, holds(window) { parts.append(L10n.text(.usageWeek, percent(window.usedPercent, language), language: language)) }
            if parts.count > 1 { lines.append(parts.joined(separator: " · ")) }
        }
        return lines
    }

    /// "421,8 k" / "421.8K": a count short enough for a menu line.
    public static func compact(_ value: Int64, _ language: Language) -> String {
        Double(value).formatted(.number.notation(.compactName).precision(.fractionLength(0 ... 1)).locale(language.locale))
    }

    /// "9 %" / "9%", from a share between 0 and 100.
    public static func percent(_ value: Double, _ language: Language) -> String {
        (value / 100).formatted(.percent.precision(.fractionLength(0)).locale(language.locale))
    }
}
