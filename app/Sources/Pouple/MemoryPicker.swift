import PoupleCore
import SwiftUI

/// The offer of facts to remember, as in the conversation window: each fact ticked as proposed, private unless the user
/// shares it, and nothing is written until they confirm.
struct MemoryPicker: View {
    @ObservedObject private var language = LanguageModel.shared

    let offer: MemoryOffer
    let busy: Bool
    let onConfirm: ([MemoryPick]) -> Void
    let onSkip: () -> Void

    @State private var draft: MemoryDraft

    init(offer: MemoryOffer, busy: Bool, onConfirm: @escaping ([MemoryPick]) -> Void, onSkip: @escaping () -> Void) {
        self.offer = offer
        self.busy = busy
        self.onConfirm = onConfirm
        self.onSkip = onSkip
        _draft = State(initialValue: MemoryDraft(offer: offer))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Label(L10n.text(.memoryQuestion), systemImage: "brain")
                .font(.system(size: 14, weight: .semibold))
                .fixedSize(horizontal: false, vertical: true)
            Text(L10n.text(.memoryHint)).font(.system(size: 11)).foregroundStyle(Theme.dim).fixedSize(horizontal: false, vertical: true).padding(.bottom, 2)
            ForEach(offer.items) { item in row(item) }
            Text(L10n.text(.memoryPrivacy)).font(.system(size: 11)).foregroundStyle(Theme.dim).fixedSize(horizontal: false, vertical: true)
            footer
        }
    }

    private func row(_ item: MemoryOffer.Item) -> some View {
        let ticked = draft.isTicked(item.id)
        let shared = draft.isShared(item.id)
        return VStack(alignment: .leading, spacing: 6) {
            Button {
                guard !busy else { return }
                draft.toggle(item.id)
            } label: {
                HStack(alignment: .top, spacing: 10) {
                    Image(systemName: ticked ? "checkmark.square.fill" : "square")
                        .font(.system(size: 15))
                        .foregroundStyle(ticked ? Theme.accent : Theme.muted)
                    VStack(alignment: .leading, spacing: 1) {
                        Text(item.fact).font(.system(size: 13.5)).multilineTextAlignment(.leading).fixedSize(horizontal: false, vertical: true)
                        Text(destination(item)).font(.system(size: 11)).foregroundStyle(Theme.dim).multilineTextAlignment(.leading)
                        if let replaces = item.replaces, !replaces.isEmpty {
                            Text(L10n.text(.memoryReplaces, replaces)).font(.system(size: 11)).foregroundStyle(Theme.dim).multilineTextAlignment(.leading).fixedSize(horizontal: false, vertical: true)
                        }
                    }
                    Spacer(minLength: 0)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityAddTraits(ticked ? .isSelected : [])
            if ticked {
                Button(shared ? L10n.text(.memoryShared) : L10n.text(.memoryPrivate)) {
                    guard !busy else { return }
                    draft.toggleShared(item.id)
                }
                .buttonStyle(PillButtonStyle(role: shared ? .primary : .quiet))
                .disabled(busy)
                .padding(.leading, 25)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        // Filled, never outlined: the chosen fact is told by a coral tint, as in the reminders.
        .background(RoundedRectangle(cornerRadius: 12, style: .continuous).fill(ticked ? Theme.accent.opacity(0.2) : Theme.tile))
    }

    private func destination(_ item: MemoryOffer.Item) -> String {
        item.note == nil ? "\(item.noteTitle) (\(L10n.text(.memoryNewNote)))" : item.noteTitle
    }

    private var footer: some View {
        HStack(spacing: 6) {
            Button(L10n.text(.memorySkip), action: onSkip).buttonStyle(PillButtonStyle(role: .quiet)).disabled(busy)
            Spacer()
            Button(confirmLabel) {
                if let picks = draft.picks() { onConfirm(picks) }
            }
            .buttonStyle(PillButtonStyle(role: .primary))
            .disabled(!draft.canConfirm || busy)
            .opacity(draft.canConfirm ? 1 : 0.5)
        }
        .padding(.top, 2)
    }

    private var confirmLabel: String {
        switch draft.chosen.count {
        case 0: L10n.text(.memoryConfirmNone)
        case 1: L10n.text(.memoryConfirmOne, 1)
        default: L10n.text(.memoryConfirmOther, draft.chosen.count)
        }
    }
}
