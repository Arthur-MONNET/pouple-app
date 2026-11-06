import AppKit
import PoupleCore
import Combine
import SwiftUI

/// A transparent panel that floats above every app, on every Space and over full-screen apps,
/// without taking the focus of whatever the user is doing.
final class FloatingPanel: NSPanel {
    init() {
        super.init(
            contentRect: NSRect(x: 0, y: 0, width: Theme.cardWidth + 2 * Theme.panelPadding, height: 80),
            styleMask: [.borderless, .nonactivatingPanel],
            backing: .buffered,
            defer: false
        )
        isFloatingPanel = true
        level = .statusBar
        collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .ignoresCycle]
        isOpaque = false
        backgroundColor = .clear
        hasShadow = false
        hidesOnDeactivate = false
        isReleasedWhenClosed = false
        isMovable = true
        // Typing in a bubble works, but merely clicking one does not pull the panel in front of the current app.
        becomesKeyOnlyIfNeeded = true
    }

    override var canBecomeKey: Bool { true }
    override var canBecomeMain: Bool { false }
}

/// The chosen corner of a screen, saved between launches.
private struct SavedPlacement {
    private static let cornerKey = "panelCorner"
    private static let displayKey = "panelDisplay"

    var corner: Corner
    /// The display the user chose; nil follows the pointer's screen.
    var displayID: CGDirectDisplayID?

    static func load() -> SavedPlacement {
        let defaults = UserDefaults.standard
        let corner = defaults.string(forKey: cornerKey).flatMap(Corner.init(rawValue:)) ?? .bottomRight
        let display = defaults.object(forKey: displayKey) as? Int
        return SavedPlacement(corner: corner, displayID: display.map { CGDirectDisplayID($0) })
    }

    func save() {
        let defaults = UserDefaults.standard
        defaults.set(corner.rawValue, forKey: Self.cornerKey)
        defaults.set(displayID.map { Int($0) }, forKey: Self.displayKey)
    }

    static func clear() {
        UserDefaults.standard.removeObject(forKey: cornerKey)
        UserDefaults.standard.removeObject(forKey: displayKey)
    }
}

private extension NSScreen {
    var displayID: CGDirectDisplayID? {
        (deviceDescription[NSDeviceDescriptionKey("NSScreenNumber")] as? NSNumber).map { CGDirectDisplayID($0.uint32Value) }
    }
}

/// Owns the panel: shows it only while there are bubbles, fits it to them, and keeps it against one corner of a screen.
/// Dragging is free, but on release the panel snaps to the nearest corner of the screen it is on.
@MainActor
final class PanelController {
    /// Distance to the screen edge; the panel's own padding comes on top.
    private static let margin: CGFloat = 6

    private let store: BubbleStore
    private let panel = FloatingPanel()
    private let host: NSHostingView<BubblesRootView>
    private var placement = SavedPlacement.load()
    private var size = NSSize(width: Theme.cardWidth + 2 * Theme.panelPadding, height: 80)
    private var subscriptions: Set<AnyCancellable> = []

    /// `corner` overrides the saved one for this run (development aid, `--corner`); nothing is saved.
    init(store: BubbleStore, corner: Corner? = nil) {
        self.store = store
        if let corner { placement.corner = corner }
        // The panel is sized here and only here: by default the hosting view would resize the window itself,
        // keeping its top edge fixed, while the bubbles must grow away from the corner chosen.
        var onSizeChange: (NSSize) -> Void = { _ in }
        host = NSHostingView(rootView: BubblesRootView(store: store, onSizeChange: { onSizeChange($0) }))
        host.sizingOptions = []
        panel.contentView = host
        store.corner = placement.corner
        onSizeChange = { [weak self] size in self?.fit(to: size) }

        store.objectWillChange
            // objectWillChange fires before the value changes: look again once it has.
            .sink { [weak self] in DispatchQueue.main.async { self?.refreshVisibility() } }
            .store(in: &subscriptions)
        NotificationCenter.default.publisher(for: .bubbleDragEnded, object: panel)
            .sink { [weak self] _ in self?.snapToNearestCorner() }
            .store(in: &subscriptions)
        // A screen that is plugged in or out must not leave the bubbles off-screen.
        NotificationCenter.default.publisher(for: NSApplication.didChangeScreenParametersNotification)
            .sink { [weak self] _ in self?.place(animated: false) }
            .store(in: &subscriptions)
        place(animated: false)
    }

    func resetPosition() {
        SavedPlacement.clear()
        placement = SavedPlacement.load()
        store.corner = placement.corner
        place(animated: true)
    }

    private func refreshVisibility() {
        let hasBubbles = !store.layout.items.isEmpty
        if hasBubbles, !panel.isVisible {
            fit(to: host.fittingSize)
            panel.orderFrontRegardless()
        } else if !hasBubbles, panel.isVisible {
            panel.orderOut(nil)
        }
    }

    private func fit(to newSize: NSSize) {
        guard newSize.width > 0, newSize.height > 0 else { return }
        size = newSize
        place(animated: false)
    }

    /// Puts the panel against its corner. The corner is fixed, so a panel that grows or shrinks never drifts.
    private func place(animated: Bool) {
        guard let area = targetScreen()?.visibleFrame else { return }
        let frame = PanelPlacement.frame(corner: placement.corner, size: size, in: area, margin: Self.margin)
        panel.setFrame(frame, display: true, animate: animated && panel.isVisible)
    }

    private func snapToNearestCorner() {
        let screens = NSScreen.screens
        let frame = panel.frame
        guard let index = PanelPlacement.screenIndex(for: frame, in: screens.map(\.visibleFrame)) else { return }
        let screen = screens[index]
        let corner = PanelPlacement.nearestCorner(to: CGPoint(x: frame.midX, y: frame.midY), in: screen.visibleFrame)
        placement = SavedPlacement(corner: corner, displayID: screen.displayID)
        placement.save()
        store.corner = corner
        place(animated: true)
    }

    /// The screen the user chose; the one under the pointer (where they are looking) when none was chosen or it is gone.
    private func targetScreen() -> NSScreen? {
        if let id = placement.displayID, let chosen = NSScreen.screens.first(where: { $0.displayID == id }) { return chosen }
        let pointer = NSEvent.mouseLocation
        return NSScreen.screens.first { $0.frame.contains(pointer) } ?? NSScreen.main ?? NSScreen.screens.first
    }
}
