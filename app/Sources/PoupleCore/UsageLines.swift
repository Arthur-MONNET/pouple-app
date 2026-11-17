import Foundation

/// The usage lines of the menu bar menu: the tokens of the day, then each AI's own usage windows.
public enum UsageLines {
    /// The names of the AIs, kept as they are in every language.
    private static let names = ["codex": "Codex", "claude": "Claude"]

    /// One line per AI, by name: its tokens of the day, then its windows. Tokens are never added up across AIs, since
    /// each has its own plan. A window that started again since it was reported is left out: the snapshot may be older.
    /// The Mac's disk and memory come last, since when either runs out the AIs fail without saying why.
    public static func lines(_ usage: AssistantUsage?, language: Language? = nil, now: Date = Date()) -> [String] {
        guard let usage else { return [] }
        let language = language ?? L10n.current
        return aiLines(usage, language: language, now: now) + [machineLine(usage.machine, language: language)].compactMap { $0 }
    }

    private static func aiLines(_ usage: AssistantUsage, language: Language, now: Date) -> [String] {
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

    /// "Mac · disk: 2.0 GB free · memory: 61%", with a warning sign when either is about to make the AIs fail.
    private static func machineLine(_ machine: MachineState?, language: Language) -> String? {
        var parts: [String] = []
        if let disk = machine?.disk {
            parts.append(L10n.text(.usageDiskFree, gigabytes(disk.freeBytes, language), language: language))
        }
        if let memory = machine?.memory {
            parts.append(L10n.text(.usageMemory, percent(memory.usedPercent, language), language: language))
        }
        guard !parts.isEmpty else { return nil }
        let critical = machine?.disk?.level == .critical || machine?.memory?.level == .critical
        let name = L10n.text(.usageMachine, language: language) + (critical ? " ⚠︎" : "")
        return ([name] + parts).joined(separator: " · ")
    }

    /// "2,0 Go" / "2.0 GB": a size in gigabytes, with one decimal under 100.
    public static func gigabytes(_ bytes: Int64, _ language: Language) -> String {
        let value = Double(bytes) / Double(1 << 30)
        let number = value.formatted(.number.precision(.fractionLength(value < 100 ? 1 : 0)).locale(language.locale))
        return L10n.text(.usageGigabytes, number, language: language)
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
