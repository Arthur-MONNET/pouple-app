import PoupleCore
import SwiftUI

/// The multiple-choice card of the web app: lettered options, a free-text answer, Back.
/// Picking an option answers at once (next question, or send on the last one); only the typed answer has a button.
struct QuestionWizard: View {
    @ObservedObject private var language = LanguageModel.shared
    private static let other = "__other"

    let questions: [AssistantQuestion]
    let busy: Bool
    let onSubmit: ([QuestionAnswer]) -> Void

    @State private var step = 0
    @State private var choices: [String: String] = [:]
    @State private var others: [String: String] = [:]
    @FocusState private var otherFocused: Bool

    private var question: AssistantQuestion { questions[min(step, questions.count - 1)] }
    private var isLast: Bool { step == questions.count - 1 }

    private var canContinue: Bool {
        guard let choice = choices[question.id] else { return false }
        return choice != Self.other || !(others[question.id] ?? "").trimmingCharacters(in: .whitespaces).isEmpty
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(question.text).font(.system(size: 14, weight: .semibold)).padding(.bottom, 4).fixedSize(horizontal: false, vertical: true)
            ForEach(Array(question.options.enumerated()), id: \.element.id) { index, option in
                optionRow(letter: index, selected: choices[question.id] == option.id) {
                    guard !busy else { return }
                    choices[question.id] = option.id
                    next()
                } label: {
                    VStack(alignment: .leading, spacing: 1) {
                        Text(option.label).font(.system(size: 13.5))
                        if let description = option.description {
                            Text(description).font(.system(size: 11)).foregroundStyle(Theme.dim)
                        }
                    }
                }
            }
            otherRow
            footer
        }
    }

    private var otherRow: some View {
        HStack(spacing: 12) {
            badge(question.options.count)
            TextField(L10n.text(.wizardOther), text: Binding(get: { others[question.id] ?? "" }, set: { others[question.id] = $0; choices[question.id] = Self.other }))
                .textFieldStyle(.plain)
                .font(.system(size: 13.5))
                .focused($otherFocused)
                // Clicking into the field is enough to pick it; only its button sends.
                .onChange(of: otherFocused) { _, focused in
                    if focused { choices[question.id] = Self.other }
                }
                .onSubmit(next)
        }
        .padding(.horizontal, 12)
        .frame(minHeight: 40)
        .background(rowBackground(selected: choices[question.id] == Self.other))
    }

    private var footer: some View {
        HStack(spacing: 6) {
            ForEach(questions.indices, id: \.self) { index in
                Circle().fill(index == step ? Color.white : Color.white.opacity(0.25)).frame(width: 5, height: 5)
            }
            Spacer()
            if step > 0 { Button(L10n.text(.wizardBack)) { step -= 1 }.buttonStyle(PillButtonStyle(role: .quiet)) }
            if choices[question.id] == Self.other {
                Button(isLast ? L10n.text(.wizardSend) : L10n.text(.wizardNext), action: next)
                    .buttonStyle(PillButtonStyle(role: .primary))
                    .disabled(!canContinue || busy)
                    .opacity(canContinue ? 1 : 0.5)
            }
        }
        .padding(.top, 2)
    }

    private func next() {
        guard canContinue, !busy else { return }
        if !isLast {
            step += 1
            otherFocused = false
            return
        }
        onSubmit(questions.map { item in
            let choice = choices[item.id]
            return choice == Self.other
                ? QuestionAnswer(questionId: item.id, text: (others[item.id] ?? "").trimmingCharacters(in: .whitespaces))
                : QuestionAnswer(questionId: item.id, optionId: choice)
        })
    }

    private func optionRow(letter: Int, selected: Bool, action: @escaping () -> Void, @ViewBuilder label: () -> some View) -> some View {
        Button(action: action) {
            HStack(alignment: .top, spacing: 12) {
                badge(letter)
                label()
                Spacer(minLength: 0)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(rowBackground(selected: selected))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private func badge(_ index: Int) -> some View {
        Text(String(UnicodeScalar(UInt8(65 + min(index, 25)))))
            .font(.system(size: 11, weight: .medium, design: .monospaced))
            .foregroundStyle(Theme.muted)
            .frame(width: 22, height: 22)
            .background(RoundedRectangle(cornerRadius: 7, style: .continuous).fill(Color.white.opacity(0.09)))
    }

    private func rowBackground(selected: Bool) -> some View {
        // Filled, never outlined: the chosen option is told by a coral tint.
        RoundedRectangle(cornerRadius: 12, style: .continuous)
            .fill(selected ? Theme.accent.opacity(0.2) : Theme.tile)
    }
}
