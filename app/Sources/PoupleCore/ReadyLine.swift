import Foundation

/// The first line the core prints on its standard output: `{"ready":true,"port":N}`.
public enum ReadyLine {
    public enum Failure: Error, Equatable, LocalizedError {
        case malformed
        case notReady
        case invalidPort(Int)

        public var errorDescription: String? {
            switch self {
            case .malformed: return "The core's first line of output is not valid"
            case .notReady: return "The core did not say it is ready"
            case let .invalidPort(port): return "The core announced an unusable port (\(port))"
            }
        }
    }

    private struct Payload: Decodable {
        let ready: Bool?
        let port: Int?
    }

    /// The port the core listens on.
    public static func parse(_ line: String) -> Result<Int, Failure> {
        let trimmed = line.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let payload = try? JSONDecoder().decode(Payload.self, from: Data(trimmed.utf8)) else { return .failure(.malformed) }
        guard payload.ready == true, let port = payload.port else { return .failure(.notReady) }
        guard (1 ... 65535).contains(port) else { return .failure(.invalidPort(port)) }
        return .success(port)
    }
}

/// Splits a byte stream into lines (`\n` or `\r\n`), keeping an unfinished line until it is completed.
public struct LineSplitter: Sendable {
    private var buffer = Data()

    public init() {}

    public mutating func feed(_ data: Data) -> [String] {
        buffer.append(data)
        var lines: [String] = []
        while let newline = buffer.firstIndex(of: 0x0A) {
            var line = buffer[buffer.startIndex ..< newline]
            if line.last == 0x0D { line = line.dropLast() }
            lines.append(String(decoding: line, as: UTF8.self))
            buffer = Data(buffer[buffer.index(after: newline)...])
        }
        return lines
    }

    /// What is left when the stream ends without a final newline.
    public mutating func finish() -> String? {
        defer { buffer = Data() }
        return buffer.isEmpty ? nil : String(decoding: buffer, as: UTF8.self)
    }
}

/// Exponential delay between two restarts: 1 s, 2 s, 4 s... capped.
public struct Backoff: Equatable, Sendable {
    public var base: Duration
    public var cap: Duration

    public init(base: Duration = .seconds(1), cap: Duration = .seconds(30)) {
        self.base = base
        self.cap = cap
    }

    /// The wait after `failures` consecutive failures (1 for the first).
    public func delay(afterFailures failures: Int) -> Duration {
        guard failures > 0 else { return .zero }
        // Past 2^20 the cap has long been reached; the clamp avoids overflow.
        let factor = 1 << min(failures - 1, 20)
        return min(base * factor, cap)
    }
}
