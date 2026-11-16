import AppKit
import PoupleCore

/// The "Check for Updates…" menu item. It asks the core, which follows the channel chosen in Settings (the stable builds
/// on main, or the beta on develop), so the menu and the window's header always tell the same thing.
@MainActor
final class AppUpdateController: NSObject, NSMenuItemValidation {
    private static let instructionsURL = URL(string: "https://arthur-monnet.github.io/pouple-app/#install")!

    /// The core's API, once it runs.
    var api: () -> PoupleAPI? = { nil }
    /// Called when the user chooses "Update now", with the branch of the channel they follow.
    var onUpdateNow: (UpdateScript.Branch) -> Void = { _ in }
    private var checkTask: Task<Void, Never>?

    @objc func checkForUpdates() {
        guard checkTask == nil else { return }
        guard let api = api() else {
            show(.updateUnavailable, detail: L10n.text(.updateCoreNotReady))
            return
        }
        checkTask = Task { [weak self] in
            defer { self?.checkTask = nil }
            do {
                let status = try await api.checkUpdate()
                try Task.checkCancellation()
                self?.present(status)
            } catch {
                guard !Task.isCancelled else { return }
                self?.show(.updateFailed, detail: error.localizedDescription)
            }
        }
    }

    func cancel() {
        checkTask?.cancel()
    }

    func validateMenuItem(_ menuItem: NSMenuItem) -> Bool {
        let checking = checkTask != nil
        menuItem.title = L10n.text(checking ? .updateChecking : .updateCheck)
        return !checking
    }

    private func present(_ status: UpdateStatus) {
        guard status.supported else {
            presentUnsupported(status.message)
            return
        }
        if let error = status.error, !status.available {
            show(.updateFailed, detail: error)
            return
        }
        guard status.available else {
            let version = status.installed?.version ?? "?"
            let channel = status.isBeta ? "\n" + L10n.text(.updateChannelBeta) : ""
            show(.updateUpToDate, detail: L10n.text(.updateInstalledVersion, version) + channel)
            return
        }
        let alert = NSAlert()
        alert.messageText = L10n.text(status.isBeta ? .updateAvailableBeta : .updateAvailable)
        // What changes, then what stops the update in the source folder, in the core's own words.
        var lines = status.changes.prefix(6).map { "• " + $0 }
        if !status.blockers.isEmpty { lines.append("") }
        lines.append(contentsOf: status.blockers.map(\.message))
        alert.informativeText = lines.joined(separator: "\n")
        let canUpdate = status.canUpdateNow && UpdateLauncher.isAvailable()
        if canUpdate { alert.addButton(withTitle: L10n.text(.updateNow)) }
        alert.addButton(withTitle: L10n.text(canUpdate ? .dialogCancel : .dialogOK))
        if alert.runModal() == .alertFirstButtonReturn, canUpdate, let branch = status.branch {
            onUpdateNow(branch)
        }
    }

    /// A copy that was not installed from its sources cannot update itself: the instructions say how to reinstall.
    private func presentUnsupported(_ message: String) {
        let alert = NSAlert()
        alert.messageText = L10n.text(.updateUnavailable)
        alert.informativeText = message
        alert.addButton(withTitle: L10n.text(.updateOpenInstructions))
        alert.addButton(withTitle: L10n.text(.dialogOK))
        if alert.runModal() == .alertFirstButtonReturn, !NSWorkspace.shared.open(Self.instructionsURL) {
            show(.updateFailed, detail: L10n.text(.updateBrowserFailed))
        }
    }

    private func show(_ title: L10n.Key, detail: String) {
        let alert = NSAlert()
        alert.messageText = L10n.text(title)
        alert.informativeText = detail
        alert.addButton(withTitle: L10n.text(.dialogOK))
        alert.runModal()
    }
}
