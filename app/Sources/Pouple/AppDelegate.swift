import AppKit
import Combine
import PoupleCore
import SwiftUI

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    private let supervisor: CoreSupervisor
    private let setup = SetupMonitor()
    private let store = BubbleStore()
    private var windowController: MainWindowController?
    private var panelController: PanelController?
    private var statusController: StatusItemController?
    private var subscriptions: Set<AnyCancellable> = []

    override init() {
        let environment = ProcessInfo.processInfo.environment
        let launcher = ProcessCoreLauncher(
            executable: CoreLocator.locate(environment: environment, resourceURL: Bundle.main.resourceURL),
            environment: environment,
            logURL: Self.logURL(environment: environment)
        )
        supervisor = CoreSupervisor(launcher: launcher)
        super.init()
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        if let path = Self.argument("--render") {
            render(to: path)
            return
        }
        if let path = Self.argument("--render-icon") {
            Self.renderIcon(to: path)
            NSApp.terminate(nil)
            return
        }
        MainMenu.install(reload: { [weak self] in self?.windowController?.reload() })
        store.openApp = { [weak self] in self?.showMainWindow() }
        store.start()
        panelController = PanelController(store: store, corner: Self.argument("--corner").flatMap(Corner.init(rawValue:)))
        installStatusItem()

        supervisor.$state
            .receive(on: DispatchQueue.main)
            .sink { [weak self] in self?.coreChanged($0) }
            .store(in: &subscriptions)
        setup.$setupComplete
            .receive(on: DispatchQueue.main)
            .sink { [weak self] in
                self?.store.setSetupComplete($0 == true)
                self?.statusController?.refresh()
            }
            .store(in: &subscriptions)
        supervisor.start()

        // A start at login lives in the menu bar; opening the app by hand shows the window.
        if Self.launchedAsLoginItem() {
            NSApp.setActivationPolicy(.accessory)
        } else {
            showMainWindow()
        }
    }

    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        store.stop()
        setup.stop()
        Task {
            // Closes the core's input, which ends it; SIGTERM and SIGKILL follow if it does not.
            await supervisor.shutdown()
            NSApp.reply(toApplicationShouldTerminate: true)
        }
        return .terminateLater
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }

    /// The Dock icon, or opening the app again from Finder.
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        showMainWindow()
        return true
    }

    // MARK: - Window

    private func showMainWindow() {
        let controller = windowController ?? makeWindowController()
        controller.show()
    }

    private func makeWindowController() -> MainWindowController {
        let controller = MainWindowController()
        controller.onClose = { NSApp.setActivationPolicy(.accessory) }
        controller.onRetry = { [weak self] in self?.supervisor.retryNow() }
        controller.coreChanged(supervisor.state)
        windowController = controller
        return controller
    }

    // MARK: - Core

    private func coreChanged(_ state: CoreState) {
        windowController?.coreChanged(state)
        if case .ready = state, let connection = supervisor.connection {
            let api = PoupleAPI(connection: connection)
            store.attach(api)
            setup.attach { try await api.setupState() }
        } else {
            store.attach(nil)
            setup.attach(fetch: nil)
        }
        statusController?.refresh()
    }

    // MARK: - Menu bar

    private func installStatusItem() {
        let controller = StatusItemController()
        controller.summary = { [weak self] in
            guard let self else { return "" }
            return StatusSummary.line(core: supervisor.state, setupComplete: setup.setupComplete)
        }
        controller.bubblesHidden = { [weak self] in self?.store.muted ?? false }
        controller.onOpen = { [weak self] in self?.showMainWindow() }
        controller.onToggleBubbles = { [weak self] in self?.store.muted.toggle() }
        controller.onResetPosition = { [weak self] in self?.panelController?.resetPosition() }
        controller.onQuit = { NSApp.terminate(nil) }
        controller.refresh()
        statusController = controller
    }

    // MARK: - Helpers

    private static func argument(_ name: String) -> String? {
        let arguments = CommandLine.arguments
        return arguments.firstIndex(of: name).flatMap { arguments.indices.contains($0 + 1) ? arguments[$0 + 1] : nil }
    }

    /// Where the core's standard error goes: next to its data when `POUPLE_HOME` is set, else in the user's Logs.
    private static func logURL(environment: [String: String]) -> URL {
        if let home = environment["POUPLE_HOME"], !home.isEmpty {
            return URL(fileURLWithPath: home).appendingPathComponent("core.log")
        }
        return FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("Library/Logs/Pouple/core.log")
    }

    /// True when the system started the app because it is a login item (as opposed to the user opening it).
    private static func launchedAsLoginItem() -> Bool {
        let event = NSAppleEventManager.shared().currentAppleEvent
        return event?.eventID == AEEventID(kAEOpenApplication)
            && event?.paramDescriptor(forKeyword: AEKeyword(keyAEPropData))?.enumCodeValue == OSType(keyAELaunchedAsLogInItem)
    }

    /// Development aid: draws the bubbles of a snapshot file into a PNG (`--render out.png --snapshot file.json [--expand id]`)
    /// without a screen, so the look can be checked without screen-recording permission.
    private func render(to path: String) {
        do {
            guard let file = Self.argument("--snapshot") else { throw PoupleAPIError.invalidResponse }
            let snapshot = try SnapshotDecoder.make().decode(AssistantSnapshot.self, from: Data(contentsOf: URL(fileURLWithPath: file)))
            store.preview(snapshot)
            if let id = Self.argument("--expand") { store.expand(id) }
            let view = BubblesRootView(store: store).padding(20).background(Color(red: 0.35, green: 0.37, blue: 0.42))
            let renderer = ImageRenderer(content: view)
            renderer.scale = 2
            guard let image = renderer.nsImage, let tiff = image.tiffRepresentation,
                  let png = NSBitmapImageRep(data: tiff)?.representation(using: .png, properties: [:]) else {
                throw PoupleAPIError.invalidResponse
            }
            try png.write(to: URL(fileURLWithPath: path))
            print("rendered \(path)")
        } catch {
            print("render failed: \(error.localizedDescription)")
        }
        NSApp.terminate(nil)
    }

    /// Development aid: the menu bar glyph at 8x on a light background (`--render-icon out.png`).
    private static func renderIcon(to path: String) {
        let size: CGFloat = 144
        let icon = StatusIcon.image(size: 18)
        let image = NSImage(size: NSSize(width: size, height: size), flipped: false) { rect in
            NSColor(white: 0.92, alpha: 1).setFill()
            rect.fill()
            icon.draw(in: rect)
            return true
        }
        if let tiff = image.tiffRepresentation, let png = NSBitmapImageRep(data: tiff)?.representation(using: .png, properties: [:]) {
            try? png.write(to: URL(fileURLWithPath: path))
            print("rendered \(path)")
        }
    }
}
