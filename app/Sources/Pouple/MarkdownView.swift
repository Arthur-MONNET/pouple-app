import PoupleCore
import SwiftUI

/// Renders the assistant's Markdown. A long text scrolls inside the bubble instead of making it taller than the screen.
struct MarkdownView: View {
    let source: String
    var maxHeight: CGFloat = 200

    @State private var contentHeight: CGFloat = 0

    var body: some View {
        // Scrolling only when the text is taller than the limit: short answers stay plain, with no scroll view around them.
        if contentHeight > maxHeight {
            ScrollView(.vertical, showsIndicators: false) { blocks }.frame(height: maxHeight)
        } else {
            blocks
        }
    }

    private var blocks: some View {
        VStack(alignment: .leading, spacing: 6) {
            ForEach(Array(Markdown.blocks(source).enumerated()), id: \.offset) { _, block in
                view(for: block)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .onGeometryChange(for: CGFloat.self, of: { $0.size.height }) { contentHeight = $0 }
    }

    @ViewBuilder private func view(for block: MarkdownBlock) -> some View {
        switch block {
        case let .paragraph(text):
            Text(Markdown.inline(text)).font(.system(size: 12.5)).fixedSize(horizontal: false, vertical: true)
        case let .heading(text):
            Text(Markdown.inline(text)).font(.system(size: 13, weight: .semibold)).fixedSize(horizontal: false, vertical: true)
        case let .list(ordered, items):
            VStack(alignment: .leading, spacing: 3) {
                ForEach(Array(items.enumerated()), id: \.offset) { index, item in
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        Text(ordered ? "\(index + 1)." : "•").font(.system(size: 12.5)).foregroundStyle(Theme.muted)
                        Text(Markdown.inline(item)).font(.system(size: 12.5)).fixedSize(horizontal: false, vertical: true)
                    }
                }
            }
        case let .code(text):
            Text(text)
                .font(.system(size: 11.5, design: .monospaced))
                .textSelection(.enabled)
                .padding(8)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(RoundedRectangle(cornerRadius: 8).fill(Theme.raised))
        }
    }
}
