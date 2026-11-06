import AppKit
import PoupleCore

/// The menu bar item and its menu. The menu is rebuilt from live state every time it opens.
@MainActor
final class StatusItemController: NSObject, NSMenuDelegate {
    private let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
    private let statusLine = NSMenuItem(title: "Starting…", action: nil, keyEquivalent: "")
    private let openItem = NSMenuItem(title: "Open Pouple", action: nil, keyEquivalent: "")
    private let bubblesItem = NSMenuItem(title: "Hide bubbles", action: nil, keyEquivalent: "")
    private let loginItem = NSMenuItem(title: "Start at login", action: nil, keyEquivalent: "")

    var summary: () -> String = { "" }
    var bubblesHidden: () -> Bool = { false }
    var onOpen: () -> Void = {}
    var onToggleBubbles: () -> Void = {}
    var onResetPosition: () -> Void = {}
    var onQuit: () -> Void = {}

    override init() {
        super.init()
        item.button?.image = StatusIcon.image()
        item.button?.setAccessibilityLabel("Pouple")
        let menu = NSMenu()
        menu.delegate = self
        statusLine.isEnabled = false
        for (entry, selector) in [(openItem, #selector(open)), (bubblesItem, #selector(toggleBubbles)), (loginItem, #selector(toggleLogin))] {
            entry.action = selector
            entry.target = self
        }
        let reset = NSMenuItem(title: "Reset bubbles position", action: #selector(resetPosition), keyEquivalent: "")
        reset.target = self
        let quit = NSMenuItem(title: "Quit Pouple", action: #selector(quit), keyEquivalent: "q")
        quit.target = self
        for entry in [statusLine, .separator(), openItem, bubblesItem, reset, .separator(), loginItem, .separator(), quit] {
            menu.addItem(entry)
        }
        item.menu = menu
        refresh()
    }

    /// Updates the lines that depend on state; called whenever the menu opens and when the state changes.
    func refresh() {
        statusLine.title = summary()
        bubblesItem.title = bubblesHidden() ? "Show bubbles" : "Hide bubbles"
        let state = LoginItem.state
        loginItem.state = state == .enabled ? .on : (state == .needsApproval ? .mixed : .off)
        loginItem.title = state.detail.map { "Start at login (\($0))" } ?? "Start at login"
    }

    func menuNeedsUpdate(_ menu: NSMenu) {
        refresh()
    }

    @objc private func open() { onOpen() }
    @objc private func toggleBubbles() { onToggleBubbles() }
    @objc private func resetPosition() { onResetPosition() }
    @objc private func toggleLogin() { LoginItem.toggle(); refresh() }
    @objc private func quit() { onQuit() }
}
