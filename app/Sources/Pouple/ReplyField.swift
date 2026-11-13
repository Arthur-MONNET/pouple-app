import PoupleCore
import SwiftUI

/// A line to write to the assistant about this request, without opening Pouple. Files dropped on the bubble wait
/// above it, as small previews, until the message that sends them.
struct ReplyField: View {
    let busy: Bool
    @ObservedObject var draft: AttachmentDraft
    let onSend: (String, [String]) -> Void

    @State private var text = ""
    @ObservedObject private var language = LanguageModel.shared

    private var trimmed: String { text.trimmingCharacters(in: .whitespacesAndNewlines) }
    /// A message is a text, files, or both.
    private var canSend: Bool { !busy && !draft.uploading && (!trimmed.isEmpty || !draft.items.isEmpty) }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if !draft.items.isEmpty || draft.uploading { previews }
            if let error = draft.error {
                Label(error, systemImage: "exclamationmark.triangle.fill")
                    .font(.system(size: 11))
                    .foregroundStyle(Theme.fail)
                    .fixedSize(horizontal: false, vertical: true)
            }
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
                .buttonStyle(IconButtonStyle(look: .disc(ready: canSend)))
                .disabled(!canSend)
                .help(L10n.text(.replySend))
                .accessibilityLabel(L10n.text(.replySend))
                .padding(.trailing, 5)
                .padding(.bottom, 5)
            }
            .background(RoundedRectangle(cornerRadius: 18, style: .continuous).fill(Theme.tile))
        }
    }

    private var previews: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                ForEach(draft.items) { item in preview(item) }
                if draft.uploading { ProgressView().controlSize(.small).frame(width: 44, height: 44) }
            }
            .padding(.top, 4)
            .padding(.trailing, 4)
        }
    }

    private func preview(_ item: AttachmentDraft.Item) -> some View {
        ZStack(alignment: .topTrailing) {
            Group {
                if let thumbnail = item.thumbnail {
                    Image(nsImage: thumbnail).resizable().scaledToFill()
                } else {
                    Text(item.ref.name).font(.system(size: 9.5)).foregroundStyle(Theme.muted).lineLimit(3).padding(4)
                }
            }
            .frame(width: 44, height: 44)
            .background(Theme.tile)
            .clipShape(RoundedRectangle(cornerRadius: 9, style: .continuous))
            Button { draft.remove(item.id) } label: {
                Image(systemName: "xmark").font(.system(size: 7, weight: .bold)).foregroundStyle(Theme.muted)
                    .frame(width: 16, height: 16)
                    .background(Circle().fill(Theme.card))
                    .overlay(Circle().strokeBorder(Color.white.opacity(0.12)))
            }
            .buttonStyle(.plain)
            .offset(x: 5, y: -5)
            .help(L10n.text(.attachmentRemove, item.ref.name))
            .accessibilityLabel(L10n.text(.attachmentRemove, item.ref.name))
        }
        .padding(.top, 2)
    }

    private func send() {
        guard canSend else { return }
        onSend(trimmed, draft.ids)
        text = ""
        draft.clear()
    }
}
