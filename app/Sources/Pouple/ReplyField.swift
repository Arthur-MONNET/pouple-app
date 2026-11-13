import PoupleCore
import SwiftUI

/// A line to write to the assistant about this request, without opening Pouple.
struct ReplyField: View {
    let busy: Bool
    let onSend: (String) -> Void

    @State private var text = ""
    @ObservedObject private var language = LanguageModel.shared

    private var trimmed: String { text.trimmingCharacters(in: .whitespacesAndNewlines) }

    var body: some View {
        // One filled field with the send button inside it, no outline.
        HStack(alignment: .bottom, spacing: 8) {
            TextField(L10n.text(.replyPlaceholder), text: $text, axis: .vertical)
                .textFieldStyle(.plain)
                .font(.system(size: 12.5))
                .lineLimit(1 ... 4)
                .onSubmit(send)
                .padding(.leading, 14)
                .padding(.vertical, 8)
            Button(action: send) {
                Image(systemName: "arrow.up").font(.system(size: 11, weight: .bold))
            }
            .buttonStyle(IconButtonStyle(look: .disc(ready: !trimmed.isEmpty)))
            .disabled(trimmed.isEmpty || busy)
            .help(L10n.text(.replySend))
            .accessibilityLabel(L10n.text(.replySend))
            .padding(.trailing, 5)
            .padding(.bottom, 5)
        }
        .background(RoundedRectangle(cornerRadius: 18, style: .continuous).fill(Theme.tile))
    }

    private func send() {
        guard !trimmed.isEmpty, !busy else { return }
        onSend(trimmed)
        text = ""
    }
}
