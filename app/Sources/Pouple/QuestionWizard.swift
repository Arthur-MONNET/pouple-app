import PoupleCore
import SwiftUI

/// The multiple-choice card of the web app: lettered options, a free-text answer, Back.
/// On a single-choice question, picking an option answers at once (next question, or send on the last one) and only the
/// typed answer has a button. On a question that allows several options, picking toggles them and the button sends.
struct QuestionWizard: View {
    @ObservedObject private var language = LanguageModel.shared
    private static let other = "__other"

    let questions: [AssistantQuestion]
    let busy: Bool
    /// Ignores the whole request in one click; nil hides the button.
    var onIgnore: (() -> Void)?
    let onSubmit: ([QuestionAnswer]) -> Void

    @State private var step = 0
    /// The option picked on each single-choice question, or `other` for the typed answer.
    @State private var choices: [String: String] = [:]
    /// The options picked on each question that allows several.
    @State private var picks: [String: Set<String>] = [:]
    @State private var others: [String: String] = [:]
    @FocusState private var otherFocused: Bool

    private var question: AssistantQuestion { questions[min(step, questions.count - 1)] }
    private var isLast: Bool { step == questions.count - 1 }

    private func typed(_ item: AssistantQuestion) -> String {
        (others[item.id] ?? "").trimmingCharacters(in: .whitespaces)
    }

    private var canContinue: Bool {
        if question.allowsSeveral { return !(picks[question.id] ?? []).isEmpty || !typed(question).isEmpty }
        guard let choice = choices[question.id] else { return false }
        return choice != Self.other || !typed(question).isEmpty
    }

    /// A single choice sends on pick; the button is there for the typed answer, and always for several options.
    private var showsAction: Bool { question.allowsSeveral || choices[question.id] == Self.other }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(question.text).font(.system(size: 14, weight: .semibold)).padding(.bottom, question.allowsSeveral ? 0 : 4).fixedSize(horizontal: false, vertical: true)
            if question.allowsSeveral {
                Text(L10n.text(.wizardSeveral)).font(.system(size: 11)).foregroundStyle(Theme.dim).padding(.bottom, 4)
            }
            ForEach(Array(question.options.enumerated()), id: \.element.id) { index, option in
                optionRow(letter: index, selected: isPicked(option)) {
                    pick(option)
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

    private func isPicked(_ option: AssistantOption) -> Bool {
        question.allowsSeveral ? (picks[question.id] ?? []).contains(option.id) : choices[question.id] == option.id
    }

    private func pick(_ option: AssistantOption) {
        guard !busy else { return }
        guard question.allowsSeveral else {
            choices[question.id] = option.id
            next()
            return
        }
        var picked = picks[question.id] ?? []
        if picked.contains(option.id) { picked.remove(option.id) } else { picked.insert(option.id) }
        picks[question.id] = picked
    }

    private var otherRow: some View {
        HStack(spacing: 12) {
            badge(question.options.count)
            TextField(L10n.text(.wizardOther), text: Binding(get: { others[question.id] ?? "" }, set: { typedOther($0) }))
                .textFieldStyle(.plain)
                .font(.system(size: 13.5))
                .focused($otherFocused)
                // On a single choice, clicking into the field is enough to pick it; only its button sends.
                .onChange(of: otherFocused) { _, focused in
                    if focused, !question.allowsSeveral { choices[question.id] = Self.other }
                }
                .onSubmit(next)
        }
        .padding(.horizontal, 12)
        .frame(minHeight: 40)
        .background(rowBackground(selected: question.allowsSeveral ? !typed(question).isEmpty : choices[question.id] == Self.other))
    }

    /// On several options, the typed answer comes in addition to the picks; on a single choice, it is the choice.
    private func typedOther(_ text: String) {
        others[question.id] = text
        if !question.allowsSeveral { choices[question.id] = Self.other }
    }

    private var footer: some View {
        HStack(spacing: 6) {
            ForEach(questions.indices, id: \.self) { index in
                Circle().fill(index == step ? Color.white : Color.white.opacity(0.25)).frame(width: 5, height: 5)
            }
            if let onIgnore {
                Button(L10n.text(.bubbleIgnore), action: onIgnore).buttonStyle(PillButtonStyle(role: .quiet)).disabled(busy)
            }
            Spacer()
            if step > 0 { Button(L10n.text(.wizardBack)) { step -= 1 }.buttonStyle(PillButtonStyle(role: .quiet)) }
            if showsAction {
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
        onSubmit(questions.map(answer(to:)))
    }

    private func answer(to item: AssistantQuestion) -> QuestionAnswer {
        let text = typed(item)
        if item.allowsSeveral {
            // In the order the options are listed, so the answer reads as the question does.
            let picked = item.options.map(\.id).filter { (picks[item.id] ?? []).contains($0) }
            return QuestionAnswer(questionId: item.id, optionIds: picked, text: text.isEmpty ? nil : text)
        }
        let choice = choices[item.id]
        return choice == Self.other ? QuestionAnswer(questionId: item.id, text: text) : QuestionAnswer(questionId: item.id, optionId: choice)
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
        .accessibilityAddTraits(selected ? .isSelected : [])
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
