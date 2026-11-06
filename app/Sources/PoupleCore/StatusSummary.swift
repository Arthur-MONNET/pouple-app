import Foundation

/// The one-line state shown in the menu bar menu.
public enum StatusSummary {
    /// - Parameter setupComplete: nil until the first answer of `/api/state`.
    public static func line(core: CoreState, setupComplete: Bool?, language: Language? = nil) -> String {
        switch core {
        case .starting: return L10n.text(.statusStarting, language: language)
        case .failed: return L10n.text(.statusUnavailable, language: language)
        case .ready:
            switch setupComplete {
            case .none: return L10n.text(.statusChecking, language: language)
            case .some(true): return L10n.text(.statusConnected, language: language)
            case .some(false): return L10n.text(.statusSetupNeeded, language: language)
            }
        }
    }
}

/// Start at login, as the menu shows it.
public enum LoginItemState: Equatable, Sendable {
    case enabled
    case disabled
    /// Registered, but the user must allow it in System Settings.
    case needsApproval
    /// The system cannot find the app (an unsigned or moved build).
    case unavailable

    public var isOn: Bool { self == .enabled || self == .needsApproval }

    /// A few words that explain a state the checkbox cannot.
    public func detail(language: Language? = nil) -> String? {
        switch self {
        case .needsApproval: return L10n.text(.loginWaitingApproval, language: language)
        case .unavailable: return L10n.text(.loginUnavailable, language: language)
        case .enabled, .disabled: return nil
        }
    }
}
