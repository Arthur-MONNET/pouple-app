import AppKit
import PoupleCore

/// The menu bar item and its menu. Titles are rebuilt from live state and the current language every time the menu
/// opens, and whenever the app calls `refresh()` (a language change included).
@MainActor
final class StatusItemController: NSObject, NSMenuDelegate {
    private let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
    private let statusLine = NSMenuItem(title: "", action: nil, keyEquivalent: "")
    private let openItem = NSMenuItem(title: "", action: nil, keyEquivalent: "")
    private let bubblesItem = NSMenuItem(title: "", action: nil, keyEquivalent: "")
    private let resetItem = NSMenuItem(title: "", action: nil, keyEquivalent: "")
    private let loginItem = NSMenuItem(title: "", action: nil, keyEquivalent: "")
    private let languageItem = NSMenuItem(title: "", action: nil, keyEquivalent: "")
    private let quitItem = NSMenuItem(title: "", action: nil, keyEquivalent: "q")
    private var languageChoices: [Language: NSMenuItem] = [:]
    private var working = false
    private var dotShown = true
    private var blinkTimer: Timer?
    private static let blinkInterval: TimeInterval = 0.6

    var summary: () -> String = { "" }
    var bubblesHidden: () -> Bool = { false }
    var currentLanguage: () -> Language = { L10n.current }
    var onOpen: () -> Void = {}
    var onToggleBubbles: () -> Void = {}
    var onResetPosition: () -> Void = {}
    var onSelectLanguage: (Language) -> Void = { _ in }
    var onQuit: () -> Void = {}

    override init() {
        super.init()
        item.button?.image = StatusIcon.image()
        let menu = NSMenu()
        menu.delegate = self
        statusLine.isEnabled = false
        let actions: [(NSMenuItem, Selector)] = [
            (openItem, #selector(open)), (bubblesItem, #selector(toggleBubbles)), (resetItem, #selector(resetPosition)),
            (loginItem, #selector(toggleLogin)), (quitItem, #selector(quit)),
        ]
        for (entry, selector) in actions {
            entry.action = selector
            entry.target = self
        }
        let languageMenu = NSMenu()
        for language in Language.allCases {
            let choice = NSMenuItem(title: language.nativeName, action: #selector(selectLanguage(_:)), keyEquivalent: "")
            choice.target = self
            choice.representedObject = language.rawValue
            languageMenu.addItem(choice)
            languageChoices[language] = choice
        }
        languageItem.submenu = languageMenu
        for entry in [statusLine, .separator(), openItem, bubblesItem, resetItem, .separator(), loginItem, languageItem, .separator(), quitItem] {
            menu.addItem(entry)
        }
        item.menu = menu
        refresh()
    }

    /// Shows or hides the "working" dot on the icon. The dot blinks, unless the user asked to reduce motion.
    func setWorking(_ isWorking: Bool) {
        guard isWorking != working else { return }
        working = isWorking
        dotShown = true
        blinkTimer?.invalidate()
        blinkTimer = nil
        if isWorking, !NSWorkspace.shared.accessibilityDisplayShouldReduceMotion {
            blinkTimer = Timer.scheduledTimer(withTimeInterval: Self.blinkInterval, repeats: true) { [weak self] _ in
                MainActor.assumeIsolated {
                    self?.dotShown.toggle()
                    self?.drawIcon()
                }
            }
        }
        drawIcon()
        refresh()
    }

    private func drawIcon() {
        item.button?.image = StatusIcon.image(badge: working && dotShown)
    }

    /// Updates every title that depends on state or language.
    func refresh() {
        statusLine.title = summary()
        openItem.title = L10n.text(.menuOpen)
        bubblesItem.title = L10n.text(bubblesHidden() ? .menuShowBubbles : .menuHideBubbles)
        resetItem.title = L10n.text(.menuResetPosition)
        quitItem.title = L10n.text(.menuQuit)
        languageItem.title = L10n.text(.menuLanguage)
        let language = currentLanguage()
        for (choice, item) in languageChoices { item.state = choice == language ? .on : .off }

        let state = LoginItem.state
        loginItem.state = state == .enabled ? .on : (state == .needsApproval ? .mixed : .off)
        let title = L10n.text(.menuStartAtLogin)
        loginItem.title = state.detail().map { L10n.text(.loginWithDetail, title, $0) } ?? title

        let label = working ? "\(L10n.text(.handleName)) – \(L10n.text(.handleWorking))" : L10n.text(.handleName)
        item.button?.setAccessibilityLabel(label)
        item.button?.toolTip = label
    }

    func menuNeedsUpdate(_ menu: NSMenu) {
        refresh()
    }

    @objc private func open() { onOpen() }
    @objc private func toggleBubbles() { onToggleBubbles() }
    @objc private func resetPosition() { onResetPosition() }
    @objc private func toggleLogin() { LoginItem.toggle(); refresh() }
    @objc private func quit() { onQuit() }
    @objc private func selectLanguage(_ sender: NSMenuItem) {
        if let code = sender.representedObject as? String, let language = Language(rawValue: code) { onSelectLanguage(language) }
    }
}
