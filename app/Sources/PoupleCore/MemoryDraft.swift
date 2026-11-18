import Foundation

/// One fact the user ticked, and whether they chose to share it. A fact is private unless it is shared.
public struct MemoryPick: Equatable, Sendable {
    public let id: String
    public let shared: Bool

    public init(id: String, shared: Bool) {
        self.id = id
        self.shared = shared
    }
}

/// What the user did so far on an offer of facts to remember: the facts ticked (as the offer proposes at first) and
/// those they made shared. A new offer starts over, with every fact private.
public struct MemoryDraft: Equatable, Sendable {
    public let offer: MemoryOffer
    public private(set) var ticked: Set<String>
    public private(set) var shared: Set<String> = []

    public init(offer: MemoryOffer) {
        self.offer = offer
        ticked = Set(offer.items.filter(\.ticked).map(\.id))
    }

    public func isTicked(_ id: String) -> Bool { ticked.contains(id) }

    public func isShared(_ id: String) -> Bool { shared.contains(id) }

    public mutating func toggle(_ id: String) {
        if ticked.contains(id) { ticked.remove(id) } else { ticked.insert(id) }
    }

    public mutating func toggleShared(_ id: String) {
        if shared.contains(id) { shared.remove(id) } else { shared.insert(id) }
    }

    /// The ticked facts, in the order they are listed.
    public var chosen: [MemoryOffer.Item] { offer.items.filter { ticked.contains($0.id) } }

    public var canConfirm: Bool { !chosen.isEmpty }

    /// What to send to the core; nil when there is nothing to confirm yet. Unticking a fact drops its sharing.
    public func picks() -> [MemoryPick]? {
        guard canConfirm else { return nil }
        return chosen.map { MemoryPick(id: $0.id, shared: shared.contains($0.id)) }
    }
}
