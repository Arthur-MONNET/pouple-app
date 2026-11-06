import Foundation

/// How a request shows up as a bubble.
public enum BubbleKind: Equatable, Sendable {
    /// Codex is still investigating: a discreet one-line pill.
    case working
    /// The user must decide (a question or a proposed change). Stays until answered.
    case decision
    case failed
    /// An answer is ready; it fades away by itself.
    case result
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
    public static let resultLifetime: TimeInterval = 25
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
        switch request.status {
        case .queued, .analyzing, .applying: return .working
        case .awaitingUser: return .decision
        case .failed: return .failed
        case .ready, .done:
            let fresh = now.timeIntervalSince(request.updatedAt) < resultLifetime
            return fresh || lingering.contains(request.id) ? .result : nil
        // Ignored requests (thanks, chit-chat) never interrupt; unknown statuses stay quiet until the overlay learns them.
        case .ignored, .unknown: return nil
        }
    }
}
