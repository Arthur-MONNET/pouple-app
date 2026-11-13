import PoupleCore
import SwiftUI

/// The mascot sitting on the top edge of an expanded bubble. It is also the grip that moves the panel.
struct BubbleMascot: View {
    @ObservedObject private var language = LanguageModel.shared
    let image: NSImage

    /// The drawing for a bubble, or nil when the app runs without its bundled mascots (a development run).
    static func image(for kind: BubbleKind) -> NSImage? {
        Mascot.image(BubblePose(kind: kind).resource)
    }

    var body: some View {
        Image(nsImage: image)
            .resizable()
            .scaledToFit()
            .frame(width: Theme.mascotSize, height: Theme.mascotSize)
            .background(WindowDragArea())
            .help(L10n.text(.handleDrag))
            .accessibilityHidden(true)
    }
}
