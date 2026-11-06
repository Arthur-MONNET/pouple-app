import AppKit
import PoupleCore

/// The application menu bar (visible while the app is a regular Dock app). Without an Edit menu, copy and paste
/// do not work in the web view's text fields.
@MainActor
enum MainMenu {
    private static var reloadAction: (() -> Void)?

    /// Builds the menu bar in the current language. Call it again when the language changes.
    static func install(updates: AppUpdateController, reload: (() -> Void)? = nil) {
        if let reload { reloadAction = reload }
        func t(_ key: L10n.Key) -> String { L10n.text(key) }
        let main = NSMenu()

        let app = NSMenu(title: t(.windowTitle))
        app.addItem(withTitle: t(.appAbout), action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: "")
        let checkUpdates = NSMenuItem(title: t(.updateCheck), action: #selector(AppUpdateController.checkForUpdates), keyEquivalent: "")
        checkUpdates.target = updates
        app.addItem(checkUpdates)
        app.addItem(.separator())
        app.addItem(withTitle: t(.appHide), action: #selector(NSApplication.hide(_:)), keyEquivalent: "h")
        let hideOthers = app.addItem(withTitle: t(.appHideOthers), action: #selector(NSApplication.hideOtherApplications(_:)), keyEquivalent: "h")
        hideOthers.keyEquivalentModifierMask = [.command, .option]
        app.addItem(.separator())
        app.addItem(withTitle: t(.appQuit), action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        main.addItem(submenu(app))

        let edit = NSMenu(title: t(.editMenu))
        edit.addItem(withTitle: t(.undo), action: Selector(("undo:")), keyEquivalent: "z")
        let redo = edit.addItem(withTitle: t(.redo), action: Selector(("redo:")), keyEquivalent: "z")
        redo.keyEquivalentModifierMask = [.command, .shift]
        edit.addItem(.separator())
        edit.addItem(withTitle: t(.cut), action: #selector(NSText.cut(_:)), keyEquivalent: "x")
        edit.addItem(withTitle: t(.copy), action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        edit.addItem(withTitle: t(.paste), action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        edit.addItem(withTitle: t(.selectAll), action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        main.addItem(submenu(edit))

        let view = NSMenu(title: t(.viewMenu))
        let reloadItem = NSMenuItem(title: t(.reload), action: #selector(Handler.reload), keyEquivalent: "r")
        reloadItem.target = Handler.shared
        view.addItem(reloadItem)
        main.addItem(submenu(view))

        let window = NSMenu(title: t(.windowMenu))
        window.addItem(withTitle: t(.minimize), action: #selector(NSWindow.performMiniaturize(_:)), keyEquivalent: "m")
        window.addItem(withTitle: t(.close), action: #selector(NSWindow.performClose(_:)), keyEquivalent: "w")
        main.addItem(submenu(window))
        NSApp.windowsMenu = window

        NSApp.mainMenu = main
    }

    private static func submenu(_ menu: NSMenu) -> NSMenuItem {
        let item = NSMenuItem(title: menu.title, action: nil, keyEquivalent: "")
        item.submenu = menu
        return item
    }

    @MainActor
    private final class Handler: NSObject {
        static let shared = Handler()
        @objc func reload() { MainMenu.reloadAction?() }
    }
}
