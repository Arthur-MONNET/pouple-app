import PoupleCore
import SwiftUI

/// The multiple-choice card of the web app: lettered options, a free-text answer, Back / Next / Send.
struct QuestionWizard: View {
    private static let other = "__other"

    let questions: [AssistantQuestion]
    let busy: Bool
    let onSubmit: ([QuestionAnswer]) -> Void

    @State private var step = 0
    @State private var choices: [String: String] = [:]
    @State private var others: [String: String] = [:]

    private var question: AssistantQuestion { questions[min(step, questions.count - 1)] }
    private var isLast: Bool { step == questions.count - 1 }

    private var canContinue: Bool {
        guard let choice = choices[question.id] else { return false }
        return choice != Self.other || !(others[question.id] ?? "").trimmingCharacters(in: .whitespaces).isEmpty
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(question.text).font(.system(size: 13, weight: .semibold)).fixedSize(horizontal: false, vertical: true)
            ForEach(Array(question.options.enumerated()), id: \.element.id) { index, option in
                optionRow(letter: index, selected: choices[question.id] == option.id) {
                    choices[question.id] = option.id
                } label: {
                    VStack(alignment: .leading, spacing: 1) {
                        Text(option.label).font(.system(size: 12.5))
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
        HStack(spacing: 9) {
            badge(question.options.count)
            TextField("Something else…", text: Binding(get: { others[question.id] ?? "" }, set: { others[question.id] = $0; choices[question.id] = Self.other }))
                .textFieldStyle(.plain)
                .font(.system(size: 12.5))
                .onSubmit(next)
        }
        .padding(.horizontal, 9)
        .frame(minHeight: 34)
        .background(rowBackground(selected: choices[question.id] == Self.other))
    }

    private var footer: some View {
        HStack(spacing: 6) {
            ForEach(questions.indices, id: \.self) { index in
                Circle().fill(index == step ? Color.white : Color.white.opacity(0.25)).frame(width: 5, height: 5)
            }
            Spacer()
            if step > 0 { Button("Back") { step -= 1 }.buttonStyle(PillButtonStyle(role: .quiet)) }
            Button(isLast ? "Send" : "Next", action: next)
                .buttonStyle(PillButtonStyle(role: .primary))
                .disabled(!canContinue || busy)
                .opacity(canContinue ? 1 : 0.5)
        }
        .padding(.top, 2)
    }

    private func next() {
        guard canContinue, !busy else { return }
        if !isLast {
            step += 1
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
            HStack(alignment: .top, spacing: 9) {
                badge(letter)
                label()
                Spacer(minLength: 0)
            }
            .padding(.horizontal, 9)
            .padding(.vertical, 7)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(rowBackground(selected: selected))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private func badge(_ index: Int) -> some View {
        Text(String(UnicodeScalar(UInt8(65 + min(index, 25)))))
            .font(.system(size: 10, weight: .medium, design: .monospaced))
            .foregroundStyle(Theme.muted)
            .frame(width: 20, height: 20)
            .overlay(RoundedRectangle(cornerRadius: 5).strokeBorder(Color.white.opacity(0.2)))
    }

    private func rowBackground(selected: Bool) -> some View {
        RoundedRectangle(cornerRadius: 9)
            .fill(selected ? Color.white.opacity(0.08) : Color.clear)
            .overlay(RoundedRectangle(cornerRadius: 9).strokeBorder(selected ? Theme.accent : Theme.line))
    }
}
