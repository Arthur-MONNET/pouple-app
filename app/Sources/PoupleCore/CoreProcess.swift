import Foundation

/// A running core, as the supervisor sees it. The real one wraps `Process`; tests use a fake.
public protocol CoreProcess: AnyObject, Sendable {
    /// Lines of standard output. Finishes when the process closes it.
    var lines: AsyncStream<String> { get }
    /// The exit status once the process ended. `nil` only if the waiting task was cancelled.
    func waitForExit() async -> Int32?
    /// Closes the process's standard input, which is how the core is told to quit.
    func closeInput()
    /// SIGTERM.
    func terminate()
    /// SIGKILL.
    func kill()
}

public protocol CoreLauncher: Sendable {
    func launch(token: String) throws -> CoreProcess
}

public enum CoreLaunchError: Error, Equatable, LocalizedError {
    case binaryMissing(String)

    public var errorDescription: String? {
        switch self {
        case let .binaryMissing(path): return "The core is missing at \(path)"
        }
    }
}

/// Where the core binary is: `POUPLE_CORE_PATH` in development, `Contents/Resources/pouple-core` in the bundle.
public enum CoreLocator {
    public static let binaryName = "pouple-core"

    public static func locate(environment: [String: String], resourceURL: URL?) -> URL {
        if let override = environment["POUPLE_CORE_PATH"], !override.isEmpty {
            return URL(fileURLWithPath: (override as NSString).expandingTildeInPath)
        }
        return (resourceURL ?? URL(fileURLWithPath: ".")).appendingPathComponent(binaryName)
    }
}

/// Starts the real binary. The token goes through the environment only (never an argument, which any local user
/// can read with `ps`), and standard input stays open: the core exits when it closes.
public struct ProcessCoreLauncher: CoreLauncher {
    public let executable: URL
    public let environment: [String: String]
    /// Where the core's standard error goes; nothing is written when nil.
    public let logURL: URL?

    public init(executable: URL, environment: [String: String] = ProcessInfo.processInfo.environment, logURL: URL? = nil) {
        self.executable = executable
        self.environment = environment
        self.logURL = logURL
    }

    public func launch(token: String) throws -> CoreProcess {
        guard FileManager.default.isExecutableFile(atPath: executable.path) else {
            throw CoreLaunchError.binaryMissing(executable.path)
        }
        var env = environment
        env["POUPLE_TOKEN"] = token
        env["POUPLE_PORT"] = "0"
        return try FoundationCoreProcess(executable: executable, environment: env, logURL: logURL)
    }
}

final class FoundationCoreProcess: CoreProcess, @unchecked Sendable {
    let lines: AsyncStream<String>
    private let process = Process()
    private let input = Pipe()
    private let output = Pipe()
    private let exit = Latch<Int32>()
    private let inputLock = NSLock()
    private var inputClosed = false

    init(executable: URL, environment: [String: String], logURL: URL?) throws {
        var continuation: AsyncStream<String>.Continuation!
        lines = AsyncStream(bufferingPolicy: .unbounded) { continuation = $0 }
        let sink = continuation!

        process.executableURL = executable
        process.environment = environment
        process.standardInput = input
        process.standardOutput = output
        process.standardError = Self.logHandle(logURL) ?? FileHandle.nullDevice

        let splitter = LockedBox(LineSplitter())
        output.fileHandleForReading.readabilityHandler = { handle in
            let data = handle.availableData
            if data.isEmpty {
                handle.readabilityHandler = nil
                if let rest = splitter.mutate({ $0.finish() }) { sink.yield(rest) }
                sink.finish()
            } else {
                for line in splitter.mutate({ $0.feed(data) }) { sink.yield(line) }
            }
        }
        let status = exit
        process.terminationHandler = { status.set($0.terminationStatus) }
        try process.run()
        // The child owns its ends now; keep only ours so closing `input` is seen as end of file.
        try? input.fileHandleForReading.close()
        try? output.fileHandleForWriting.close()
    }

    func waitForExit() async -> Int32? { await exit.wait() }

    func closeInput() {
        inputLock.lock()
        defer { inputLock.unlock() }
        guard !inputClosed else { return }
        inputClosed = true
        try? input.fileHandleForWriting.close()
    }

    func terminate() {
        if process.isRunning { process.terminate() }
    }

    func kill() {
        if process.isRunning { Foundation.kill(process.processIdentifier, SIGKILL) }
    }

    private static func logHandle(_ url: URL?) -> FileHandle? {
        guard let url else { return nil }
        try? FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        if !FileManager.default.fileExists(atPath: url.path) {
            FileManager.default.createFile(atPath: url.path, contents: nil, attributes: [.posixPermissions: 0o600])
        }
        guard let handle = try? FileHandle(forWritingTo: url) else { return nil }
        _ = try? handle.seekToEnd()
        return handle
    }
}

/// A small lock around a value shared with a callback queue.
final class LockedBox<Value>: @unchecked Sendable {
    private let lock = NSLock()
    private var value: Value

    init(_ value: Value) { self.value = value }

    func mutate<T>(_ body: (inout Value) -> T) -> T {
        lock.lock()
        defer { lock.unlock() }
        return body(&value)
    }
}
