import PoupleCore
import SwiftUI

/// One request as a bubble: one line when collapsed, with its question or answer when expanded.
struct BubbleCard: View {
    @ObservedObject var store: BubbleStore
    let item: BubbleItem
    let expanded: Bool

    private var request: AssistantRequest { item.request }
    private var busy: Bool { store.busy.contains(request.id) }

    var body: some View {
        Group {
            if expanded || item.kind == .working { full } else { collapsed }
        }
        .frame(width: Theme.cardWidth, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 16, style: .continuous).fill(Theme.card.opacity(0.96)))
        .overlay(RoundedRectangle(cornerRadius: 16, style: .continuous).strokeBorder(border))
        .shadow(color: .black.opacity(0.4), radius: 14, y: 6)
        .onHover { store.setHover(request.id, $0) }
    }

    private var border: Color {
        switch item.kind {
        case .decision: return Theme.accent.opacity(0.55)
        case .failed: return Theme.fail.opacity(0.55)
        case .working, .result: return Theme.line
        }
    }

    // MARK: - Collapsed

    private var collapsed: some View {
        Button { store.toggle(request.id) } label: {
            HStack(spacing: 8) {
                icon
                Text(request.title).font(.system(size: 12, weight: .medium)).lineLimit(1)
                Spacer(minLength: 4)
                Text(badge).font(.system(size: 11)).foregroundStyle(badgeColor)
                Image(systemName: "chevron.up").font(.system(size: 9, weight: .bold)).foregroundStyle(Theme.dim)
            }
            .padding(.horizontal, 12)
            .frame(height: 38)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    // MARK: - Expanded

    private var full: some View {
        VStack(alignment: .leading, spacing: 10) {
            header
            quote
            content
            if item.kind != .working {
                if !steps.isEmpty { ActivityLog(steps: steps) }
                ReplyField(busy: busy) { store.send(request, $0) }
            }
            if let error = store.errors[request.id] {
                Label(error, systemImage: "exclamationmark.triangle.fill").font(.system(size: 11)).foregroundStyle(Theme.fail).fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(12)
    }

    private var header: some View {
        HStack(spacing: 8) {
            icon
            VStack(alignment: .leading, spacing: 1) {
                Text(request.title).font(.system(size: 12.5, weight: .semibold)).lineLimit(1)
                Text(badge).font(.system(size: 10.5)).foregroundStyle(badgeColor)
            }
            Spacer(minLength: 4)
            if item.kind != .working {
                Button { store.toggle(request.id) } label: { Image(systemName: "chevron.down") }
                    .help("Collapse")
            }
            Button { store.dismiss(request) } label: { Image(systemName: "xmark") }
                .help("Close this bubble")
        }
        .buttonStyle(IconButtonStyle())
        .background(WindowDragArea())
    }

    private var quote: some View {
        Text(request.text)
            .font(.system(size: 12))
            .foregroundStyle(Theme.muted)
            .lineLimit(3)
            .fixedSize(horizontal: false, vertical: true)
            .padding(.vertical, 6)
            .padding(.horizontal, 10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: 8).fill(Theme.raised))
    }

    @ViewBuilder private var content: some View {
        switch item.kind {
        case .working:
            HStack(spacing: 8) {
                ProgressView().controlSize(.small)
                Text(lastStep ?? "Investigating…")
                    .font(.system(size: 11, design: lastStep == nil ? .default : .monospaced))
                    .foregroundStyle(Theme.dim)
                    .lineLimit(1)
            }
        case .decision:
            if let summary = request.summary { MarkdownView(source: summary) }
            decision
        case .failed:
            Label(request.error ?? "Something went wrong", systemImage: "exclamationmark.triangle.fill")
                .font(.system(size: 12))
                .foregroundStyle(Theme.fail)
                .fixedSize(horizontal: false, vertical: true)
            HStack {
                Button("Investigate again") { store.retry(request) }.buttonStyle(PillButtonStyle(role: .primary)).disabled(busy)
                openButton
            }
        case .result:
            if let summary = request.summary { MarkdownView(source: summary) }
            if let text = request.draftReply { replyBox(text) }
            HStack {
                Button("Done") { store.dismiss(request) }.buttonStyle(PillButtonStyle(role: .primary))
                openButton
            }
        }
    }

    @ViewBuilder private var decision: some View {
        if let proposal = request.proposal {
            VStack(alignment: .leading, spacing: 8) {
                Text("Apply this change in \(proposal.projectId)?").font(.system(size: 13, weight: .semibold))
                Text(proposal.title).font(.system(size: 12, weight: .medium))
                Text(proposal.description).font(.system(size: 12)).foregroundStyle(Theme.muted).lineLimit(6).fixedSize(horizontal: false, vertical: true)
                HStack {
                    Button("Make the change") { store.decide(request, approve: true) }.buttonStyle(PillButtonStyle(role: .primary)).disabled(busy)
                    Button("Not now") { store.decide(request, approve: false) }.buttonStyle(PillButtonStyle()).disabled(busy)
                    Spacer()
                    openButton
                }
            }
        } else if !request.questions.isEmpty {
            QuestionWizard(questions: request.questions, busy: busy) { store.answer(request, $0) }
                .id(request.questions.map(\.id).joined(separator: ","))
        }
    }

    private var openButton: some View {
        Button("Open Pouple") { store.openPouple() }.buttonStyle(PillButtonStyle(role: .quiet))
    }

    private func replyBox(_ text: String) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text("Suggested reply").font(.system(size: 11, weight: .medium)).foregroundStyle(Theme.dim)
                Spacer()
                Button("Copy") { store.copy(text) }.buttonStyle(PillButtonStyle(role: .quiet))
            }
            Text(text).font(.system(size: 12.5)).textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.line))
    }

    // MARK: - Bits

    private var steps: [ChatEntry] { request.entries.filter { $0.kind == .step } }

    private var lastStep: String? { steps.last?.text }

    private var badge: String {
        switch item.kind {
        case .working: return "Investigating…"
        case .decision: return request.proposal != nil ? "Approve a change" : "Needs your answer"
        case .failed: return "Failed"
        case .result: return request.status == .done ? "Change applied" : "Answer ready"
        }
    }

    private var badgeColor: Color {
        switch item.kind {
        case .working: return Theme.dim
        case .decision: return Theme.accent
        case .failed: return Theme.fail
        case .result: return Theme.ok
        }
    }

    private var icon: some View {
        Image(systemName: symbol)
            .font(.system(size: 12, weight: .semibold))
            .foregroundStyle(badgeColor == Theme.dim ? Theme.muted : badgeColor)
            .frame(width: 22, height: 22)
            .background(Circle().fill(Color.white.opacity(0.08)))
    }

    private var symbol: String {
        switch request.source.origin {
        case .poupleAction: return "bolt.fill"
        case .noteToSelf: return "note.text"
        case .directMessage: return "envelope"
        case .groupMessage: return "person.2"
        case .broadcast: return "megaphone"
        case .keyword: return "magnifyingglass"
        case .threadReply: return "arrowshape.turn.up.left"
        case .mention, .channelMessage, .unknown: return "at"
        }
    }
}

struct IconButtonStyle: ButtonStyle {
    /// Highlighted with the accent color, for the main action of a row.
    var filled = false

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 10, weight: .bold))
            .foregroundStyle(filled ? Theme.onAccent : Theme.muted)
            .frame(width: 22, height: 22)
            .background(Circle().fill(filled ? Theme.accent : Color.white.opacity(configuration.isPressed ? 0.16 : 0.06)))
    }
}
