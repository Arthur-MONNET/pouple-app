import AppKit
import PoupleCore

/// The in-app "Update" button: opens Terminal on a short script that waits for Pouple to quit, pulls the code and runs
/// the install script again, then quits the app. The connections stay: the Keychain and Pouple's data folder are not touched.
@MainActor
final class UpdateLauncher {
    private var launching = false

    /// Where the install script leaves its record (the same folder the core keeps its data in).
    static var dataDirectory: URL {
        if let home = ProcessInfo.processInfo.environment["POUPLE_HOME"], !home.isEmpty { return URL(fileURLWithPath: home, isDirectory: true) }
        return FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("Library/Application Support/Pouple", isDirectory: true)
    }

    /// Whether this copy can update itself (it was installed from a source checkout that is still there).
    static func isAvailable() -> Bool {
        if case .success = currentPlan() { return true }
        return false
    }

    private static func currentPlan() -> Result<UpdateScript.InstallRecord, UpdateScript.Problem> {
        let fileManager = FileManager.default
        let data = try? Data(contentsOf: dataDirectory.appendingPathComponent("install.json"))
        return UpdateScript.plan(
            recordData: data,
            directoryExists: { var isDirectory: ObjCBool = false; return fileManager.fileExists(atPath: $0, isDirectory: &isDirectory) && isDirectory.boolValue },
            fileExists: { fileManager.isReadableFile(atPath: $0) }
        )
    }

    func launch() {
        guard !launching else { return }
        switch Self.currentPlan() {
        case let .failure(problem):
            present(problem)
        case let .success(record):
            launching = true
            do {
                let script = try writeScript(for: record)
                openInTerminal(script)
            } catch {
                launching = false
                show(L10n.text(.updateTerminalFailed, error.localizedDescription))
            }
        }
    }

    private func writeScript(for record: UpdateScript.InstallRecord) throws -> URL {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent("pouple-update-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
        let file = folder.appendingPathComponent("update.command")
        let messages = UpdateScript.Messages(
            quitting: L10n.text(.scriptQuitting), fetching: L10n.text(.scriptFetching), building: L10n.text(.scriptBuilding),
            done: L10n.text(.scriptDone), notQuit: L10n.text(.scriptNotQuit), badOrigin: L10n.text(.scriptBadOrigin),
            pullFailed: L10n.text(.scriptPullFailed), installFailed: L10n.text(.scriptInstallFailed), close: L10n.text(.scriptClose)
        )
        let text = UpdateScript.content(sourceRoot: record.sourceRoot, appPID: ProcessInfo.processInfo.processIdentifier, messages: messages)
        guard FileManager.default.createFile(atPath: file.path, contents: Data(text.utf8), attributes: [.posixPermissions: 0o700]) else {
            throw CocoaError(.fileWriteUnknown)
        }
        return file
    }

    private func openInTerminal(_ script: URL) {
        let terminal = URL(fileURLWithPath: "/System/Applications/Utilities/Terminal.app")
        NSWorkspace.shared.open([script], withApplicationAt: terminal, configuration: NSWorkspace.OpenConfiguration()) { [weak self] _, error in
            DispatchQueue.main.async {
                guard let self else { return }
                if let error {
                    self.launching = false
                    self.show(L10n.text(.updateTerminalFailed, error.localizedDescription))
                } else {
                    // The script waits for this process to end, then does the work: the app only has to get out of the way.
                    NSApp.terminate(nil)
                }
            }
        }
    }

    private func present(_ problem: UpdateScript.Problem) {
        switch problem {
        case .notInstalledFromSource, .invalidRecord: show(L10n.text(.updateNotFromSource))
        case let .checkoutMissing(path): show(L10n.text(.updateCheckoutMissing, path))
        }
    }

    private func show(_ detail: String) {
        let alert = NSAlert()
        alert.messageText = L10n.text(.updateFailed)
        alert.informativeText = detail
        alert.addButton(withTitle: L10n.text(.dialogOK))
        alert.runModal()
    }
}
