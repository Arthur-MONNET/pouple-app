import AppKit
import SwiftUI

extension Notification.Name {
    /// Posted, with the window as object, when the user starts a drag on a `WindowDragArea`.
    static let bubbleDragBegan = Notification.Name("bubbleDragBegan")
    /// Posted, with the window as object, when the user lets go of a drag started on a `WindowDragArea`.
    static let bubbleDragEnded = Notification.Name("bubbleDragEnded")
}

/// A transparent area that moves the whole panel when dragged, like the title bar of a window.
struct WindowDragArea: NSViewRepresentable {
    func makeNSView(context: Context) -> NSView { DragView() }
    func updateNSView(_ view: NSView, context: Context) {}

    private final class DragView: NSView {
        override var mouseDownCanMoveWindow: Bool { true }
        override func mouseDown(with event: NSEvent) {
            guard let window else { return }
            NotificationCenter.default.post(name: .bubbleDragBegan, object: window)
            // Returns when the mouse button is released.
            window.performDrag(with: event)
            NotificationCenter.default.post(name: .bubbleDragEnded, object: window)
        }
    }
}
