import PoupleCore
import SwiftUI

/// The whole content of the panel: a column of bubbles with the newest at the bottom, on a transparent background.
struct BubblesRootView: View {
    @ObservedObject var store: BubbleStore
    /// Called with the size the bubbles want, so the owner can fit the window to them.
    var onSizeChange: (CGSize) -> Void = { _ in }

    var body: some View {
        let layout = store.layout
        let corner = store.corner
        // The newest bubble sits next to the corner and the grip on the far side, whichever corner it is.
        let items = corner.isTop ? Array(layout.items.reversed()) : layout.items
        VStack(alignment: corner.isLeft ? .leading : .trailing, spacing: 8) {
            if !layout.items.isEmpty, !corner.isTop { handle(layout) }
            ForEach(items) { item in
                BubbleCard(store: store, item: item, expanded: store.expandedID == item.id)
                    .transition(.move(edge: corner.isTop ? .top : .bottom).combined(with: .opacity))
            }
            if !layout.items.isEmpty, corner.isTop { handle(layout) }
        }
        .animation(.spring(response: 0.35, dampingFraction: 0.85), value: layout.items)
        .padding(Theme.panelPadding)
        .frame(width: Theme.cardWidth + 2 * Theme.panelPadding, alignment: corner.isLeft ? .leading : .trailing)
        .fixedSize(horizontal: false, vertical: true)
        .environment(\.colorScheme, .dark)
        .tint(Theme.accent)
        .onGeometryChange(for: CGSize.self, of: { $0.size }, action: onSizeChange)
    }

    /// The grip to move the panel, small enough to stay out of the way.
    private func handle(_ layout: BubbleLayout) -> some View {
        HStack(spacing: 6) {
            Image(systemName: "line.3.horizontal").font(.system(size: 9, weight: .bold))
            Text(layout.overflow > 0 ? "Pouple · +\(layout.overflow) more" : "Pouple").font(.system(size: 10, weight: .medium))
            if !store.connected { Image(systemName: "wifi.slash").font(.system(size: 9)) }
        }
        .foregroundStyle(Theme.muted)
        .padding(.horizontal, 10)
        .frame(height: 20)
        .background(WindowDragArea())
        .background(Capsule().fill(Theme.card.opacity(0.9)))
        .overlay(Capsule().strokeBorder(Theme.line))
        .help("Drag to move")
    }
}
