import Foundation

public enum CoreState: Equatable, Sendable {
    case starting
    case ready(port: Int)
    case failed(CoreFailure)
}

/// Starts the core, waits for its ready line, restarts it with an exponential backoff when it dies, and stops it
/// cleanly: close its standard input, then SIGTERM, then SIGKILL after a grace period each.
@MainActor
public final class CoreSupervisor: ObservableObject {
    public struct Timing: Sendable {
        /// How long the core has to print its ready line.
        public var readyTimeout: Duration
        /// How long it has to quit after each step of the shutdown.
        public var grace: Duration
        /// A core that stayed up this long is healthy again: the next crash restarts without delay.
        public var stableAfter: Duration
        public var backoff: Backoff

        public init(
            readyTimeout: Duration = .seconds(15),
            grace: Duration = .seconds(3),
            stableAfter: Duration = .seconds(30),
            backoff: Backoff = Backoff()
        ) {
            self.readyTimeout = readyTimeout
            self.grace = grace
            self.stableAfter = stableAfter
            self.backoff = backoff
        }
    }

    @Published public private(set) var state: CoreState = .starting
    /// Where to reach the running core and the token for it. Nil unless the state is `ready`.
    public private(set) var connection: CoreConnection?

    private let launcher: CoreLauncher
    private let timing: Timing
    private let makeToken: @Sendable () -> String
    private var loop: Task<Void, Never>?
    private var current: CoreProcess?
    private var wake: Latch<Void>?
    private var stopping = false
    private var resetFailures = false

    public init(launcher: CoreLauncher, timing: Timing = Timing(), makeToken: @escaping @Sendable () -> String = { CoreSupervisor.randomToken() }) {
        self.launcher = launcher
        self.timing = timing
        self.makeToken = makeToken
    }

    /// 32 random bytes as hex, fresh at every launch of the core.
    public nonisolated static func randomToken() -> String {
        var generator = SystemRandomNumberGenerator()
        return (0 ..< 32).map { _ in String(format: "%02x", UInt8.random(in: .min ... .max, using: &generator)) }.joined()
    }

    public func start() {
        guard loop == nil, !stopping else { return }
        loop = Task { [weak self] in await self?.supervise() }
    }

    /// Skips the wait before the next restart (the Retry button).
    public func retryNow() {
        resetFailures = true
        wake?.set(())
    }

    /// Stops the core and the supervision. Returns when the process is gone.
    public func shutdown() async {
        stopping = true
        wake?.set(())
        if let process = current { await stop(process) }
        await loop?.value
        loop = nil
        connection = nil
    }

    // MARK: - Loop

    private func supervise() async {
        var failures = 0
        while !stopping {
            state = .starting
            connection = nil
            let token = makeToken()
            let failure: CoreFailure
            do {
                let process = try launcher.launch(token: token)
                current = process
                let reader = ReadyReader(process: process)
                switch await reader.firstLine(timeout: timing.readyTimeout) {
                case let .ready(port):
                    connection = CoreConnection(port: port, token: token)
                    state = .ready(port: port)
                    let readyAt = ContinuousClock.now
                    let status = await process.waitForExit()
                    if ContinuousClock.now - readyAt >= timing.stableAfter { failures = 0 }
                    failure = .stopped(status: status)
                case let .failed(reason):
                    await stop(process)
                    failure = reason
                }
                reader.cancel()
                current = nil
            } catch let error as CoreLaunchError {
                current = nil
                switch error {
                case let .binaryMissing(path): failure = .binaryMissing(path: path)
                case .invalidToken: failure = .other("Invalid core token")
                }
            } catch {
                current = nil
                failure = .other(error.localizedDescription)
            }
            if stopping { break }
            if resetFailures { failures = 0; resetFailures = false }
            failures += 1
            connection = nil
            state = .failed(failure)
            let pause = Latch<Void>()
            wake = pause
            _ = await withTimeout(timing.backoff.delay(afterFailures: failures)) { await pause.wait() }
            wake = nil
            if resetFailures { failures = 0; resetFailures = false }
        }
    }

    /// Close the input, then SIGTERM, then SIGKILL, waiting `grace` after each.
    private func stop(_ process: CoreProcess) async {
        process.closeInput()
        if await withTimeout(timing.grace, { await process.waitForExit() }) != nil { return }
        process.terminate()
        if await withTimeout(timing.grace, { await process.waitForExit() }) != nil { return }
        process.kill()
        _ = await process.waitForExit()
    }
}

/// Reads the ready line of one process and keeps draining its output afterwards, so the core never blocks on a
/// full pipe. One task owns the stream for the whole life of the process.
private enum ReadyOutcome: Sendable {
    case ready(Int)
    case failed(CoreFailure)
}

private final class ReadyReader: @unchecked Sendable {
    private let outcome = Latch<ReadyOutcome>()
    private var task: Task<Void, Never>?

    init(process: CoreProcess) {
        let outcome = outcome
        task = Task {
            var first = true
            for await line in process.lines {
                guard first else { continue }
                first = false
                switch ReadyLine.parse(line) {
                case let .success(port): outcome.set(.ready(port))
                case let .failure(error): outcome.set(.failed(.readyLine(error)))
                }
            }
            outcome.set(.failed(.closedBeforeReady))
        }
    }

    func firstLine(timeout: Duration) async -> ReadyOutcome {
        let outcome = outcome
        let result = await withTimeout(timeout) { await outcome.wait() }
        return result ?? .failed(.timeout(seconds: Int(timeout.components.seconds)))
    }

    func cancel() { task?.cancel() }
}
