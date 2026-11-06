import AppKit

// The delegate decides at launch whether the window opens (a start at login stays in the menu bar).
let application = NSApplication.shared
let delegate = MainActor.assumeIsolated { AppDelegate() }
application.delegate = delegate
application.setActivationPolicy(.regular)
application.run()
