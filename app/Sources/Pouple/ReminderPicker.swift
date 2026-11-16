import PoupleCore
import SwiftUI

/// The suggestion of reminders, as in the conversation window: a tick and a moment for each, and nothing is set until the
/// user confirms. Ticking or changing the moment of an option chooses it.
struct ReminderPicker: View {
    @ObservedObject private var language = LanguageModel.shared

    let offer: ReminderOffer
    let busy: Bool
    let onConfirm: ([ReminderPick]) -> Void
    let onSkip: () -> Void

    @State private var draft: ReminderDraft

    init(offer: ReminderOffer, busy: Bool, onConfirm: @escaping ([ReminderPick]) -> Void, onSkip: @escaping () -> Void) {
        self.offer = offer
        self.busy = busy
        self.onConfirm = onConfirm
        self.onSkip = onSkip
        _draft = State(initialValue: ReminderDraft(offer: offer))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Label(L10n.text(.reminderQuestion), systemImage: "bell")
                .font(.system(size: 14, weight: .semibold))
                .fixedSize(horizontal: false, vertical: true)
            Text(L10n.text(.reminderHint)).font(.system(size: 11)).foregroundStyle(Theme.dim).fixedSize(horizontal: false, vertical: true).padding(.bottom, 2)
            ForEach(offer.items) { item in row(item) }
            if !draft.chosen.isEmpty, !draft.allUpcoming() {
                Text(L10n.text(.reminderPast)).font(.system(size: 11)).foregroundStyle(Theme.fail).fixedSize(horizontal: false, vertical: true)
            }
            footer
        }
    }

    private func row(_ item: ReminderOffer.Item) -> some View {
        let ticked = draft.isTicked(item.id)
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
                        Text(item.label).font(.system(size: 13.5)).multilineTextAlignment(.leading)
                        if let note = item.note, !note.isEmpty {
                            Text(note).font(.system(size: 11)).foregroundStyle(Theme.dim).multilineTextAlignment(.leading)
                        }
                    }
                    Spacer(minLength: 0)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityAddTraits(ticked ? .isSelected : [])
            DatePicker("", selection: Binding(get: { draft.time(of: item.id) }, set: { draft.setTime($0, of: item.id) }), displayedComponents: [.date, .hourAndMinute])
                .labelsHidden()
                .datePickerStyle(.compact)
                .disabled(busy)
                .accessibilityLabel(L10n.text(.reminderTimeLabel, item.label))
                .padding(.leading, 25)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        // Filled, never outlined: the chosen option is told by a coral tint, as in the question card.
        .background(RoundedRectangle(cornerRadius: 12, style: .continuous).fill(ticked ? Theme.accent.opacity(0.2) : Theme.tile))
    }

    private var footer: some View {
        HStack(spacing: 6) {
            Button(L10n.text(.reminderSkip), action: onSkip).buttonStyle(PillButtonStyle(role: .quiet)).disabled(busy)
            Spacer()
            Button(confirmLabel) {
                if let picks = draft.picks() { onConfirm(picks) }
            }
            .buttonStyle(PillButtonStyle(role: .primary))
            .disabled(!draft.canConfirm() || busy)
            .opacity(draft.canConfirm() ? 1 : 0.5)
        }
        .padding(.top, 2)
    }

    private var confirmLabel: String {
        switch draft.chosen.count {
        case 0: L10n.text(.reminderConfirmNone)
        case 1: L10n.text(.reminderConfirmOne, 1)
        default: L10n.text(.reminderConfirmOther, draft.chosen.count)
        }
    }
}
