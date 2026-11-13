import Foundation

/// How a request shows up as a bubble.
public enum BubbleKind: Equatable, Sendable {
    /// The user must decide (a question or a proposed change). Stays until answered.
    case decision
    /// The assistant works on a request that already reached the user: the bubble shows the work.
    case working
    case failed
    /// An answer is ready. It stays until the user says it is done.
    case result
    /// A change was made: a short confirmation that fades by itself.
    case done
}

public struct BubbleItem: Equatable, Identifiable, Sendable {
    public let request: AssistantRequest
    public let kind: BubbleKind
    public var id: String { request.id }
}

public struct BubbleLayout: Equatable, Sendable {
    /// Oldest first: the newest bubble sits at the bottom, next to the screen corner.
    public let items: [BubbleItem]
    /// Visible bubbles that did not fit and are not shown.
    public let overflow: Int

    public init(items: [BubbleItem], overflow: Int) {
        self.items = items
        self.overflow = overflow
    }

    public static let empty = BubbleLayout(items: [], overflow: 0)
}

public enum BubbleVisibility {
    /// How long the confirmation of a change made stays, unless the pointer is on it.
    public static let confirmationLifetime: TimeInterval = 10
    public static let maxBubbles = 5

    /// - Parameters:
    ///   - dismissed: request id → `updatedAt` at the moment the user closed its bubble; it comes back only if the request changes again.
    ///   - lingering: ids whose bubble must not fade (the pointer is on it).
    public static func layout(
        requests: [AssistantRequest],
        now: Date,
        dismissed: [String: Date],
        lingering: Set<String> = []
    ) -> BubbleLayout {
        let items = requests
            .sorted { $0.receivedAt < $1.receivedAt }
            .compactMap { request -> BubbleItem? in
                if let closedAt = dismissed[request.id], request.updatedAt <= closedAt { return nil }
                guard let kind = kind(of: request, now: now, lingering: lingering) else { return nil }
                return BubbleItem(request: request, kind: kind)
            }
        let shown = Array(items.suffix(maxBubbles))
        return BubbleLayout(items: shown, overflow: items.count - shown.count)
    }

    private static func kind(of request: AssistantRequest, now: Date, lingering: Set<String>) -> BubbleKind? {
        if request.isClosed { return nil }
        switch request.status {
        // Before the request first reaches the user, the work is silent: only the menu bar icon shows it. Once the user
        // has been in the conversation, they watch the work in the bubble.
        case .queued, .analyzing, .applying: return request.isEngaged ? .working : nil
        case .awaitingUser: return .decision
        case .failed: return .failed
        // An answer stays until the user says it is done. One from before the core said so never comes back.
        case .ready: return request.isEngaged ? .result : nil
        case .done:
            let fresh = now.timeIntervalSince(request.updatedAt) < confirmationLifetime
            return fresh || lingering.contains(request.id) ? .done : nil
        // Ignored requests (thanks, chit-chat) never interrupt; unknown statuses stay quiet until the overlay learns them.
        case .ignored, .unknown: return nil
        }
    }
}
