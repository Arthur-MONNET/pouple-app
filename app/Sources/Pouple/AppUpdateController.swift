import AppKit
import PoupleCore

@MainActor
final class AppUpdateController: NSObject, NSMenuItemValidation {
    private let checker = UpdateChecker()
    private var checkTask: Task<Void, Never>?

    @objc func checkForUpdates() {
        guard checkTask == nil else { return }
        checkTask = Task { [weak self] in
            guard let self else { return }
            defer { checkTask = nil }
            let installedVersion = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? ""
            do {
                let result = try await checker.check(installedVersion: installedVersion)
                try Task.checkCancellation()
                present(result, installedVersion: installedVersion)
            } catch {
                guard !Task.isCancelled else { return }
                present(error)
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

    private func present(_ result: UpdateChecker.Result, installedVersion: String) {
        switch result {
        case .upToDate:
            show(.updateUpToDate, detail: L10n.text(.updateInstalledVersion, installedVersion))
        case let .available(version, instructionsURL):
            let alert = NSAlert()
            alert.messageText = L10n.text(.updateAvailable, version)
            alert.informativeText = L10n.text(.updateInstallInstructions)
            alert.addButton(withTitle: L10n.text(.updateOpenInstructions))
            alert.addButton(withTitle: L10n.text(.dialogCancel))
            if alert.runModal() == .alertFirstButtonReturn, !NSWorkspace.shared.open(instructionsURL) {
                show(.updateFailed, detail: L10n.text(.updateBrowserFailed))
            }
        }
    }

    private func present(_ error: Error) {
        switch error as? UpdateChecker.Failure {
        case .invalidInstalledVersion:
            show(.updateUnavailable, detail: L10n.text(.updateInvalidInstalledVersion))
        case .releaseUnavailable:
            show(.updateUnavailable, detail: L10n.text(.updateReleaseUnavailable))
        default:
            show(.updateFailed, detail: L10n.text(.updateTryAgain))
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
