import PoupleCore
import SwiftUI

/// One request as a bubble: one line when collapsed, with its question or answer when expanded.
struct BubbleCard: View {
    @ObservedObject var store: BubbleStore
    @ObservedObject private var language = LanguageModel.shared
    let item: BubbleItem
    let expanded: Bool
    /// The files dropped on this bubble, waiting for the next message.
    @StateObject private var draft = AttachmentDraft()
    /// A file is being dragged over the card.
    @State private var dropTargeted = false

    private var request: AssistantRequest { item.request }
    private var busy: Bool { store.busy.contains(request.id) }

    /// The mascot of an expanded bubble; nil when collapsed, or when the app has no bundled mascots.
    private var mascot: NSImage? { expanded ? BubbleMascot.image(for: item.kind) : nil }

    var body: some View {
        let mascot = mascot
        card(withMascot: mascot != nil)
            // Room above the card for the part of the mascot that rises over its edge.
            .padding(.top, mascot == nil ? 0 : Theme.mascotOverhang)
            // On the side of the screen the panel is anchored to: left corners, left; right corners, right.
            .overlay(alignment: store.corner.isLeft ? .topLeading : .topTrailing) {
                if let mascot {
                    BubbleMascot(image: mascot)
                        .padding(store.corner.isLeft ? .leading : .trailing, 14)
                }
            }
            .onHover { store.setHover(request.id, $0) }
    }

    /// Files can be dropped on an open bubble that can be written to: not on a one-line one, nor while the work runs.
    private var acceptsFiles: Bool { expanded && item.kind != .working }

    private var dropHint: some View {
        RoundedRectangle(cornerRadius: 20, style: .continuous)
            .strokeBorder(Theme.accent, style: StrokeStyle(lineWidth: 2, dash: [6, 4]))
            .background(RoundedRectangle(cornerRadius: 20, style: .continuous).fill(Theme.card.opacity(0.85)))
            .overlay(Text(L10n.text(.attachmentDropHint)).font(.system(size: 13, weight: .medium)))
            .allowsHitTesting(false)
    }

    /// One solid surface in the app's background color: the state is told by the dot and the word, not by a border.
    private func card(withMascot: Bool) -> some View {
        Group {
            if expanded { full(withMascot: withMascot) } else { collapsed }
        }
        .frame(width: Theme.cardWidth, alignment: .leading)
        // Anywhere on the card that is not a control moves the panel: the drag area sits under the content.
        .background { if !store.previewing { WindowDragArea() } }
        .overlay { if dropTargeted && acceptsFiles { dropHint } }
        .takingFiles(acceptsFiles && !store.previewing, isTargeted: $dropTargeted) { providers in
            AttachmentDraft.fileURLs(of: providers) { urls in
                draft.add(urls, upload: store.uploader(for: request))
            }
        }
        .background(RoundedRectangle(cornerRadius: 20, style: .continuous).fill(Theme.card))
        // A hairline only, so the card still lifts off a dark wallpaper.
        .overlay(RoundedRectangle(cornerRadius: 20, style: .continuous).strokeBorder(Color.white.opacity(0.06)))
        .shadow(color: .black.opacity(0.45), radius: Theme.shadowRadius, y: Theme.shadowOffset)
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

    private func full(withMascot: Bool) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            header
            quote
            content
            // While the assistant works, the steps are the content, and there is nothing to write to it yet.
            if item.kind != .working {
                if !steps.isEmpty { ActivityLog(steps: steps) }
                ReplyField(busy: busy, draft: draft) { store.send(request, $0, attachmentIds: $1) }
            }
            if let error = store.errors[request.id] {
                Label(error, systemImage: "exclamationmark.triangle.fill").font(.system(size: 11)).foregroundStyle(Theme.fail).fixedSize(horizontal: false, vertical: true)
            }
        }
        // The mascot rises 24 pt into the card: the header starts below it.
        .padding(EdgeInsets(top: withMascot ? 30 : 14, leading: 14, bottom: 14, trailing: 14))
    }

    private var header: some View {
        HStack(spacing: 10) {
            icon
            VStack(alignment: .leading, spacing: 3) {
                Text(request.title).font(.system(size: 13, weight: .semibold)).lineLimit(1)
                HStack(spacing: 6) {
                    Circle().fill(badgeColor).frame(width: 6, height: 6)
                    Text(badge).fontWeight(.medium).foregroundStyle(badgeColor)
                    Text("· " + L10n.relativeTime(from: request.receivedAt, to: store.now)).foregroundStyle(Theme.dim)
                }
                .font(.system(size: 11))
                .lineLimit(1)
            }
            Spacer(minLength: 4)
            Button { store.toggle(request.id) } label: { Image(systemName: "chevron.down") }
                .help(L10n.text(.bubbleCollapse))
                .accessibilityLabel(L10n.text(.bubbleCollapse))
            Button { store.dismiss(request) } label: { Image(systemName: "xmark") }
                .help(L10n.text(.bubbleClose))
                .accessibilityLabel(L10n.text(.bubbleClose))
        }
        .buttonStyle(IconButtonStyle())
    }

    private var quote: some View {
        Text(request.text)
            .font(.system(size: 12.5))
            .foregroundStyle(Theme.muted)
            .lineLimit(3)
            .fixedSize(horizontal: false, vertical: true)
            .padding(.vertical, 9)
            .padding(.horizontal, 12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: 12, style: .continuous).fill(Theme.raised))
    }

    @ViewBuilder private var content: some View {
        switch item.kind {
        case .decision:
            if let summary = request.summary { MarkdownView(source: summary) }
            decision
        case .working:
            work
        case .failed:
            Label(request.error ?? L10n.text(.bubbleSomethingWrong), systemImage: "exclamationmark.triangle.fill")
                .font(.system(size: 12))
                .foregroundStyle(Theme.fail)
                .fixedSize(horizontal: false, vertical: true)
            HStack {
                // When the AI in use reached its usage limit, the other one taking over is the way forward: investigating
                // again with the AI at its limit would only fail again.
                if let name = request.switchToName {
                    Button(L10n.text(.bubbleContinueWith, name)) { store.continueWith(request) }
                        .buttonStyle(PillButtonStyle(role: .primary))
                        .fixedSize()
                        .disabled(busy)
                } else {
                    Button(L10n.text(.bubbleRetry)) { store.retry(request) }.buttonStyle(PillButtonStyle(role: .primary)).disabled(busy)
                }
                openButton
            }
        case .result:
            if let summary = request.summary { MarkdownView(source: summary) }
            if let text = request.draftReply { replyBox(text) }
            HStack {
                Button(L10n.text(.bubbleDone)) { store.dismiss(request) }.buttonStyle(PillButtonStyle(role: .primary))
                openButton
            }
        case .done:
            if let summary = request.summary { MarkdownView(source: summary) }
            openButton
        }
    }

    /// The assistant at work on what the user answered: its latest steps, and a way to stop it.
    private var work: some View {
        VStack(alignment: .leading, spacing: 8) {
            if !steps.isEmpty {
                VStack(alignment: .leading, spacing: 3) {
                    ForEach(steps.suffix(3)) { step in
                        Text(step.text)
                            .font(.system(size: 11, design: step.text.hasPrefix("$ ") ? .monospaced : .default))
                            .foregroundStyle(Theme.dim)
                            .lineLimit(1)
                    }
                }
            }
            HStack(spacing: 8) {
                Circle().fill(Theme.working).frame(width: 6, height: 6)
                Text(L10n.text(.bubbleWorking)).font(.system(size: 12)).foregroundStyle(Theme.muted)
                Spacer()
                Button(L10n.text(.bubbleStop)) { store.stop(request) }.buttonStyle(PillButtonStyle(role: .destructive)).disabled(busy)
            }
        }
    }

    @ViewBuilder private var decision: some View {
        if let proposal = request.proposal {
            VStack(alignment: .leading, spacing: 8) {
                Text(L10n.text(.bubbleApplyIn, proposal.projectId)).font(.system(size: 13, weight: .semibold))
                Text(proposal.title).font(.system(size: 12, weight: .medium))
                Text(proposal.description).font(.system(size: 12)).foregroundStyle(Theme.muted).lineLimit(6).fixedSize(horizontal: false, vertical: true)
                HStack {
                    Button(L10n.text(.bubbleApply)) { store.decide(request, approve: true) }.buttonStyle(PillButtonStyle(role: .primary)).disabled(busy)
                    Button(L10n.text(.bubbleNotNow)) { store.decide(request, approve: false) }.buttonStyle(PillButtonStyle()).disabled(busy)
                    Spacer()
                    openButton
                }
            }
        } else if !request.questions.isEmpty {
            QuestionWizard(questions: request.questions, busy: busy, onIgnore: { store.ignore(request) }) { store.answer(request, $0) }
                .id(request.questions.map(\.id).joined(separator: ","))
        }
    }

    private var openButton: some View {
        Button(L10n.text(.bubbleOpen)) { store.openPouple(request) }.buttonStyle(PillButtonStyle(role: .quiet))
    }

    private func replyBox(_ text: String) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text(L10n.text(.bubbleSuggestedReply)).font(.system(size: 11, weight: .medium)).foregroundStyle(Theme.dim)
                Spacer()
                Button(L10n.text(.bubbleCopy)) { store.copy(text) }.buttonStyle(PillButtonStyle(role: .quiet))
            }
            Text(text).font(.system(size: 12.5)).textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 12, style: .continuous).fill(Theme.tile))
    }

    // MARK: - Bits

    private var steps: [ChatEntry] { request.entries.filter { $0.kind == .step } }


    private var badge: String {
        switch item.kind {
        case .decision: return request.proposal != nil ? L10n.text(.badgeApprove) : L10n.text(.badgeNeedsAnswer)
        case .working: return L10n.text(.badgeWorking)
        case .failed: return L10n.text(.badgeFailed)
        case .result: return L10n.text(.badgeAnswerReady)
        case .done: return L10n.text(.badgeApplied)
        }
    }

    private var badgeColor: Color {
        switch item.kind {
        case .decision: return Theme.accent
        case .working: return Theme.working
        case .failed: return Theme.fail
        case .result, .done: return Theme.ok
        }
    }

    private var icon: some View {
        Image(systemName: symbol)
            .font(.system(size: 12, weight: .semibold))
            .foregroundStyle(badgeColor)
            .frame(width: 22, height: 22)
            .background(Circle().fill(Color.white.opacity(0.08)))
    }

    private var symbol: String {
        switch request.source.origin {
        case .poupleAction: return "bolt.fill"
        case .pipeline: return "gearshape.2"
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
    enum Look {
        /// A bare glyph, for the controls of a header.
        case plain
        /// A quiet disc that turns coral when the action is available.
        case disc(ready: Bool)
    }

    var look: Look = .plain

    func makeBody(configuration: Configuration) -> some View {
        let ready: Bool = if case .disc(let ready) = look { ready } else { false }
        configuration.label
            .font(.system(size: 10, weight: .bold))
            .foregroundStyle(ready ? Theme.onAccent : Theme.muted.opacity(configuration.isPressed ? 1 : 0.8))
            .frame(width: 22, height: 22)
            .background {
                if case .disc = look {
                    Circle().fill(ready ? Theme.accent : Color.white.opacity(0.1))
                }
            }
            .contentShape(Rectangle())
    }
}
