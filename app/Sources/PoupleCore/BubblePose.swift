/// The mascot drawing an expanded bubble sits with: it follows what the bubble asks of the user.
public enum BubblePose: Equatable, Sendable, CaseIterable {
    /// Waiting for a decision: looking, magnifier up.
    case waiting
    /// Working on what the user answered: juggling.
    case working
    /// An answer is ready or a change was made.
    case done
    /// Something went wrong.
    case failed

    public init(kind: BubbleKind) {
        switch kind {
        case .decision: self = .waiting
        case .working: self = .working
        case .result, .done: self = .done
        case .failed: self = .failed
        }
    }

    /// The bundled drawing, `mascot-<resource>.svg`.
    public var resource: String {
        switch self {
        case .waiting: return "investigate"
        case .working: return "multitask"
        case .done: return "done"
        case .failed: return "oops"
        }
    }
}
