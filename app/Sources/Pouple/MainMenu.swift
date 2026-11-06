import AppKit

/// The application menu bar (visible while the app is a regular Dock app). Without an Edit menu, copy and paste
/// do not work in the web view's text fields.
@MainActor
enum MainMenu {
    private static var reloadAction: (() -> Void)?

    static func install(reload: @escaping () -> Void) {
        reloadAction = reload
        let main = NSMenu()

        let app = NSMenu(title: "Pouple")
        app.addItem(withTitle: "About Pouple", action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: "")
        app.addItem(.separator())
        app.addItem(withTitle: "Hide Pouple", action: #selector(NSApplication.hide(_:)), keyEquivalent: "h")
        let hideOthers = app.addItem(withTitle: "Hide Others", action: #selector(NSApplication.hideOtherApplications(_:)), keyEquivalent: "h")
        hideOthers.keyEquivalentModifierMask = [.command, .option]
        app.addItem(.separator())
        app.addItem(withTitle: "Quit Pouple", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        main.addItem(submenu(app))

        let edit = NSMenu(title: "Edit")
        edit.addItem(withTitle: "Undo", action: Selector(("undo:")), keyEquivalent: "z")
        let redo = edit.addItem(withTitle: "Redo", action: Selector(("redo:")), keyEquivalent: "z")
        redo.keyEquivalentModifierMask = [.command, .shift]
        edit.addItem(.separator())
        edit.addItem(withTitle: "Cut", action: #selector(NSText.cut(_:)), keyEquivalent: "x")
        edit.addItem(withTitle: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        edit.addItem(withTitle: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        edit.addItem(withTitle: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        main.addItem(submenu(edit))

        let view = NSMenu(title: "View")
        let reloadItem = NSMenuItem(title: "Reload", action: #selector(Handler.reload), keyEquivalent: "r")
        reloadItem.target = Handler.shared
        view.addItem(reloadItem)
        main.addItem(submenu(view))

        let window = NSMenu(title: "Window")
        window.addItem(withTitle: "Minimize", action: #selector(NSWindow.performMiniaturize(_:)), keyEquivalent: "m")
        window.addItem(withTitle: "Close", action: #selector(NSWindow.performClose(_:)), keyEquivalent: "w")
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
