import PoupleCore
import SwiftUI

/// The whole content of the panel: a column of bubbles with the newest at the bottom, on a transparent background.
struct BubblesRootView: View {
    @ObservedObject var store: BubbleStore
    @ObservedObject private var language = LanguageModel.shared
    /// Called with the size the bubbles want, so the owner can fit the window to them.
    var onSizeChange: (CGSize) -> Void = { _ in }

    var body: some View {
        let layout = store.layout
        let corner = store.corner
        // The newest bubble sits next to the corner and the grip on the far side, whichever corner it is.
        let items = corner.isTop ? Array(layout.items.reversed()) : layout.items
        VStack(alignment: corner.isLeft ? .leading : .trailing, spacing: 8) {
            if showsHandle(layout), !corner.isTop { handle(layout) }
            ForEach(items) { item in
                BubbleCard(store: store, item: item, expanded: store.expandedID == item.id)
                    .transition(.move(edge: corner.isTop ? .top : .bottom).combined(with: .opacity))
            }
            if showsHandle(layout), corner.isTop { handle(layout) }
        }
        .animation(.spring(response: 0.35, dampingFraction: 0.85), value: layout.items)
        .padding(Theme.panelPadding)
        .frame(width: Theme.cardWidth + 2 * Theme.panelPadding, alignment: corner.isLeft ? .leading : .trailing)
        .fixedSize(horizontal: false, vertical: true)
        .environment(\.colorScheme, .dark)
        .tint(Theme.accent)
        .onGeometryChange(for: CGSize.self, of: { $0.size }, action: onSizeChange)
    }

    /// The mascot of the expanded bubble is the grip to move the panel. A small label takes over when it cannot be:
    /// no bubble is expanded, the mascots are not bundled, or there is something to say (hidden bubbles, no connection).
    private func showsHandle(_ layout: BubbleLayout) -> Bool {
        guard !layout.items.isEmpty else { return false }
        let expanded = layout.items.first { $0.id == store.expandedID }
        let hasMascot = expanded.map { BubbleMascot.image(for: $0.kind) != nil } ?? false
        return !hasMascot || layout.overflow > 0 || !store.connected
    }

    private func handle(_ layout: BubbleLayout) -> some View {
        HStack(spacing: 6) {
            Text(layout.overflow > 0 ? L10n.text(.handleOverflow, layout.overflow) : L10n.text(.handleName)).font(.system(size: 10.5, weight: .medium))
            if !store.connected { Image(systemName: "wifi.slash").font(.system(size: 9)) }
        }
        .foregroundStyle(Theme.muted)
        .padding(.horizontal, 12)
        .frame(height: 22)
        .background(WindowDragArea())
        .background(Capsule().fill(Theme.card))
        .shadow(color: .black.opacity(0.4), radius: 10, y: 4)
        .help(L10n.text(.handleDrag))
        .accessibilityLabel(L10n.text(.handleDrag))
    }
}
