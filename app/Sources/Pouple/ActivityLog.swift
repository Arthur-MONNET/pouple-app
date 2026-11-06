import PoupleCore
import SwiftUI

/// What the assistant did, folded away by default: commands it ran and the context it relied on.
struct ActivityLog: View {
    let steps: [ChatEntry]
    @State private var open = false
    @ObservedObject private var language = LanguageModel.shared

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Button { open.toggle() } label: {
                HStack(spacing: 4) {
                    Image(systemName: open ? "chevron.down" : "chevron.right").font(.system(size: 8, weight: .bold))
                    Text(L10n.plural(one: .activityOne, other: .activityOther, count: steps.count)).font(.system(size: 10.5))
                }
                .foregroundStyle(Theme.dim)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

            if open {
                ScrollView(.vertical) {
                    VStack(alignment: .leading, spacing: 2) {
                        ForEach(steps) { step in
                            Text(step.text)
                                .font(.system(size: 10.5, design: step.text.hasPrefix("$ ") ? .monospaced : .default))
                                .foregroundStyle(Theme.dim)
                                .lineLimit(2)
                                .frame(maxWidth: .infinity, alignment: .leading)
                        }
                    }
                    .padding(.leading, 10)
                    .overlay(alignment: .leading) { Rectangle().fill(Theme.line).frame(width: 1) }
                }
                .frame(maxHeight: 110)
            }
        }
    }
}
