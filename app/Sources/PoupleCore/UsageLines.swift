import Foundation

/// The usage lines of the menu bar menu: the tokens of the day, then each AI's own usage windows.
public enum UsageLines {
    /// The names of the AIs, kept as they are in every language.
    private static let names = ["codex": "Codex", "claude": "Claude"]

    /// One line per AI, by name: its tokens of the day, then its windows. Tokens are never added up across AIs, since
    /// each has its own plan. A window that started again since it was reported is left out: the snapshot may be older.
    public static func lines(_ usage: AssistantUsage?, language: Language? = nil, now: Date = Date()) -> [String] {
        guard let usage else { return [] }
        let language = language ?? L10n.current
        let ids = Set((usage.limits ?? [:]).keys).union((usage.tokensByAI ?? [:]).keys)
        let holds = { (window: UsageWindow) in window.resetsAt.map { $0 > now } ?? true }
        return ids.map { (name: names[$0] ?? $0, id: $0) }.sorted { $0.name < $1.name }.compactMap { provider in
            var parts: [String] = []
            if let tokens = usage.tokensByAI?[provider.id], tokens.total > 0 {
                parts.append(L10n.text(.usageTokens, compact(tokens.total, language), language: language))
            }
            let limits = usage.limits?[provider.id]
            if let window = limits?.fiveHour, holds(window) { parts.append(L10n.text(.usageFiveHour, percent(window.usedPercent, language), language: language)) }
            if let window = limits?.week, holds(window) { parts.append(L10n.text(.usageWeek, percent(window.usedPercent, language), language: language)) }
            return parts.isEmpty ? nil : ([provider.name] + parts).joined(separator: " · ")
        }
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
