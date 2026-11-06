import Foundation

/// Asks the core every few seconds whether the required connectors are set up. The bubbles and the menu follow it.
@MainActor
public final class SetupMonitor: ObservableObject {
    /// nil until the first answer, and again whenever there is no core to ask.
    @Published public private(set) var setupComplete: Bool?
    /// The interface language the core reports; nil until it says (or when there is no core).
    @Published public private(set) var language: Language?

    private let interval: Duration
    private var task: Task<Void, Never>?

    public init(interval: Duration = .seconds(5)) {
        self.interval = interval
    }

    /// Starts (or replaces) the polling with `fetch`; nil stops it and forgets the last answer.
    public func attach(fetch: (@Sendable () async throws -> SetupState)?) {
        task?.cancel()
        task = nil
        setupComplete = nil
        language = nil
        guard let fetch else { return }
        task = Task { [weak self, interval] in
            while !Task.isCancelled {
                // A failed call keeps the last answer: the supervisor reports a dead core on its own.
                if let state = try? await fetch(), !Task.isCancelled {
                    self?.setupComplete = state.setupComplete
                    if let language = state.language { self?.language = language }
                }
                try? await Task.sleep(for: interval)
            }
        }
    }

    public func stop() {
        attach(fetch: nil)
    }
}
