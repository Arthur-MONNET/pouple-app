import AppKit
import PoupleCore
import ServiceManagement

/// "Start at login", backed by the system's login item registry for this very app.
enum LoginItem {
    static var state: LoginItemState {
        switch SMAppService.mainApp.status {
        case .enabled: return .enabled
        case .requiresApproval: return .needsApproval
        case .notRegistered: return .disabled
        case .notFound: return .unavailable
        @unknown default: return .unavailable
        }
    }

    /// Flips the setting. Registering can fail (an unsigned build, an app outside /Applications): the error is shown,
    /// and the menu keeps reflecting what the system really says.
    @MainActor
    static func toggle() {
        switch state {
        case .needsApproval:
            SMAppService.openSystemSettingsLoginItems()
        case .enabled:
            attempt { try SMAppService.mainApp.unregister() }
        case .disabled, .unavailable:
            attempt { try SMAppService.mainApp.register() }
        }
    }

    @MainActor
    private static func attempt(_ change: () throws -> Void) {
        do {
            try change()
            if state == .needsApproval { SMAppService.openSystemSettingsLoginItems() }
        } catch {
            let alert = NSAlert()
            alert.messageText = L10n.text(.loginErrorTitle)
            alert.informativeText = L10n.text(.loginErrorBody, error.localizedDescription)
            alert.runModal()
        }
    }
}
