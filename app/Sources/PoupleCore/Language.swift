import Foundation

/// The interface language of the app. The core owns the setting (`uiLanguage`); the shell follows it.
public enum Language: String, CaseIterable, Codable, Sendable {
    case fr
    case en

    public static let `default`: Language = .fr

    /// Accepts "fr", "en", and regional forms ("fr-FR", "en_US"); nil for anything else.
    public init?(code: String) {
        let base = code.lowercased().split(whereSeparator: { $0 == "-" || $0 == "_" }).first.map(String.init) ?? ""
        self.init(rawValue: base)
    }

    /// The name of the language in itself, as the language menu shows it.
    public var nativeName: String {
        switch self {
        case .fr: return "Français"
        case .en: return "English"
        }
    }

    public var locale: Locale { Locale(identifier: self == .fr ? "fr_FR" : "en_US") }
}

/// The language the views read, and the object they observe to redraw when it changes.
@MainActor
public final class LanguageModel: ObservableObject {
    public static let shared = LanguageModel(defaults: .standard)

    @Published public private(set) var language: Language

    private let defaults: UserDefaults?
    private static let cacheKey = "interfaceLanguage"

    /// With `defaults`, the last language is remembered, so the first screen before the core answers is already right.
    public init(defaults: UserDefaults? = nil) {
        self.defaults = defaults
        language = defaults?.string(forKey: Self.cacheKey).flatMap(Language.init(code:)) ?? .default
        L10n.current = language
    }

    public func set(_ newLanguage: Language) {
        guard newLanguage != language else { return }
        language = newLanguage
        L10n.current = newLanguage
        defaults?.set(newLanguage.rawValue, forKey: Self.cacheKey)
    }
}
