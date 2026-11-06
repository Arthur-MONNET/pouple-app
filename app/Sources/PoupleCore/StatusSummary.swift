import Foundation

/// The one-line state shown in the menu bar menu.
public enum StatusSummary {
    /// - Parameter setupComplete: nil until the first answer of `/api/state`.
    public static func line(core: CoreState, setupComplete: Bool?) -> String {
        switch core {
        case .starting: return "Starting…"
        case .failed: return "Core unavailable"
        case .ready:
            switch setupComplete {
            case .none: return "Checking…"
            case .some(true): return "Connected"
            case .some(false): return "Setup needed"
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

    public var detail: String? {
        switch self {
        case .needsApproval: return "Waiting for approval in System Settings"
        case .unavailable: return "Unavailable for this build"
        case .enabled, .disabled: return nil
        }
    }
}
