import AppKit
import Combine
import Foundation
import PoupleCore

/// What the views show and do: the latest snapshot from the core, which bubbles are visible, and the actions on them.
@MainActor
final class BubbleStore: ObservableObject {
    @Published private(set) var snapshot: AssistantSnapshot?
    @Published private(set) var connected = false
    @Published private(set) var now = Date()
    @Published private(set) var dismissed: [String: Date] = [:]
    @Published private(set) var errors: [String: String] = [:]
    @Published private(set) var busy: Set<String> = []
    /// The one bubble shown in full; the others are a single line, so the stack stays short.
    @Published private(set) var expandedID: String?
    /// The user hid the bubbles from the menu. Remembered between launches.
    @Published var muted = UserDefaults.standard.bool(forKey: BubbleStore.mutedKey) {
        didSet { UserDefaults.standard.set(muted, forKey: Self.mutedKey) }
    }
    /// The screen corner the panel is anchored to; bubbles grow away from it.
    @Published var corner: Corner = .bottomRight
    /// Bubbles only exist once the required connectors are set up.
    @Published private(set) var setupComplete = false
    @Published private(set) var api: PoupleAPI?

    /// Brings the main window to the front ("Open Pouple" in a bubble).
    /// Opens the main window, on the conversation of a request when one is named.
    var openApp: (String?) -> Void = { _ in }

    private var knownIDs: Set<String> = []
    private var hovered: Set<String> = []
    private var lingerUntil: [String: Date] = [:]
    private var tickTask: Task<Void, Never>?
    private var streamTask: Task<Void, Never>?
    private var streaming: PoupleAPI?

    private static let mutedKey = "bubblesHidden"
    private static let lingerSeconds: TimeInterval = 8
    private static let reconnectDelay: Duration = .seconds(3)

    private var languageSubscription: AnyCancellable?

    init(api: PoupleAPI? = nil) {
        self.api = api
        // The views read their texts through L10n: tell them to redraw when the language changes.
        languageSubscription = LanguageModel.shared.$language
            .dropFirst()
            .sink { [weak self] _ in self?.objectWillChange.send() }
    }

    var layout: BubbleLayout {
        guard !muted, setupComplete, api != nil, let snapshot else { return .empty }
        let lingering = Set(lingerUntil.filter { $0.value > now }.keys)
        return BubbleVisibility.layout(requests: snapshot.requests, now: now, dismissed: dismissed, lingering: lingering)
    }

    func start() {
        tickTask = Task { [weak self] in await self?.tick() }
        reconcile()
    }

    func stop() {
        tickTask?.cancel()
        tickTask = nil
        streamTask?.cancel()
        streamTask = nil
        streaming = nil
    }

    // MARK: - Connection

    /// Points the store at the running core, or at nothing (the core is starting, restarting or failed).
    func attach(_ api: PoupleAPI?) {
        guard api?.connection != self.api?.connection else { return }
        self.api = api
        if api == nil {
            snapshot = nil
            connected = false
        }
        reconcile()
    }

    func setSetupComplete(_ complete: Bool) {
        guard complete != setupComplete else { return }
        setupComplete = complete
        reconcile()
    }

    /// Follows the stream only while there is a core to talk to and the setup is done.
    private func reconcile() {
        guard tickTask != nil else { return }
        let wanted = setupComplete ? api : nil
        if streaming?.connection == wanted?.connection { return }
        streamTask?.cancel()
        streamTask = nil
        streaming = wanted
        guard let wanted else {
            snapshot = nil
            connected = false
            return
        }
        streamTask = Task { [weak self] in await self?.followServer(wanted) }
    }

    // MARK: - Actions

    /// Closes the bubble (Done, or the close button): at once here, and in the core, so it stays closed after a restart and
    /// comes back only when the request needs the user again.
    func dismiss(_ request: AssistantRequest) {
        hide(request) { api in
            do {
                try await api.close(requestId: request.id, seen: request.updatedAt)
            } catch let PoupleAPIError.http(status, _) where status == 409 {
                // The request changed since the user looked at it: the new state stays open, and that is not an error.
            }
        }
    }

    /// Ignores the request: the conversation is over. Only possible while the assistant is not working on it.
    func ignore(_ request: AssistantRequest) {
        hide(request) { api in try await api.ignore(requestId: request.id) }
    }

    /// Hides the bubble at once, and shows it again, with the error, if the core refuses.
    private func hide(_ request: AssistantRequest, _ work: @escaping (PoupleAPI) async throws -> Void) {
        guard !busy.contains(request.id), api != nil else { return }
        dismissed[request.id] = request.updatedAt
        perform(request.id, work, onFailure: { [weak self] in self?.dismissed[request.id] = nil })
    }

    /// Switches to the AI the request offers, and investigates it again with it.
    func continueWith(_ request: AssistantRequest) {
        guard let provider = request.switchTo else { return }
        perform(request.id) { api in try await api.continueWith(requestId: request.id, provider: provider) }
    }

    func stop(_ request: AssistantRequest) {
        perform(request.id) { api in try await api.stop(requestId: request.id) }
    }

    func expand(_ id: String) {
        expandedID = id
    }

    func toggle(_ id: String) {
        expandedID = expandedID == id ? nil : id
    }

    func setHover(_ id: String, _ isHovering: Bool) {
        if isHovering { hovered.insert(id) } else { hovered.remove(id) }
    }

    func answer(_ request: AssistantRequest, _ answers: [QuestionAnswer]) {
        perform(request.id) { api in try await api.answer(requestId: request.id, answers: answers) }
    }

    func decide(_ request: AssistantRequest, approve: Bool) {
        perform(request.id) { api in try await api.decideProposal(requestId: request.id, approve: approve) }
    }

    func send(_ request: AssistantRequest, _ text: String, attachmentIds: [String] = []) {
        perform(request.id) { api in try await api.message(requestId: request.id, text: text, attachmentIds: attachmentIds) }
    }

    /// Uploads a file to the core for the next message of a request; nil api means no core to talk to.
    func uploader(for request: AssistantRequest) -> (String, Data) async throws -> AttachmentRef {
        { [weak self] name, data in
            guard let api = self?.api else { throw PoupleAPIError.invalidResponse }
            return try await api.uploadAttachment(requestId: request.id, name: name, data: data)
        }
    }

    func retry(_ request: AssistantRequest) {
        perform(request.id) { api in try await api.retry(requestId: request.id) }
    }

    func openPouple(_ request: AssistantRequest) {
        openApp(request.id)
    }

    func copy(_ text: String) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
    }

    // MARK: - Server

    private func perform(_ id: String, _ work: @escaping (PoupleAPI) async throws -> Void, onFailure: (() -> Void)? = nil) {
        guard !busy.contains(id), let api else { return }
        busy.insert(id)
        errors[id] = nil
        Task {
            do {
                try await work(api)
            } catch {
                errors[id] = error.localizedDescription
                onFailure?()
            }
            busy.remove(id)
        }
    }

    private func followServer(_ api: PoupleAPI) async {
        let stream = SnapshotStream(request: api.streamRequest())
        while !Task.isCancelled {
            do {
                for try await snapshot in stream.snapshots() { apply(snapshot) }
            } catch {
                // The core restarting is routine: wait and reconnect (the supervisor swaps the connection if needed).
            }
            if Task.isCancelled { return }
            connected = false
            try? await Task.sleep(for: Self.reconnectDelay)
        }
    }

    func apply(_ snapshot: AssistantSnapshot) {
        self.snapshot = snapshot
        connected = true
        now = Date()
        // A bubble that just appeared takes the room.
        let items = layout.items
        let appeared = items.last { !knownIDs.contains($0.id) }
        knownIDs.formUnion(items.map(\.id))
        if let appeared {
            expandedID = appeared.id
        } else if !items.contains(where: { $0.id == expandedID }) {
            expandedID = items.last?.id
        }
    }

    /// A development render (`--render`) draws the bubbles into an image: native drag areas cannot be drawn there.
    private(set) var previewing = false

    /// Development aid (`--render`): shows the given snapshot as if a core had sent it.
    func preview(_ snapshot: AssistantSnapshot) {
        previewing = true
        setupComplete = true
        api = api ?? PoupleAPI(connection: CoreConnection(port: 1, token: "preview"))
        apply(snapshot)
    }

    private func tick() async {
        while !Task.isCancelled {
            try? await Task.sleep(for: .seconds(1))
            now = Date()
            for id in hovered { lingerUntil[id] = now.addingTimeInterval(Self.lingerSeconds) }
            lingerUntil = lingerUntil.filter { $0.value > now }
        }
    }
}
