import Foundation

public struct ServerSentEvent: Equatable, Sendable {
    public let name: String
    public let data: String
}

/// Line-by-line parser of the `text/event-stream` format (only what the core sends: `event`, `data`, comments).
public struct ServerSentEventParser: Sendable {
    private var name = "message"
    private var data: [String] = []

    public init() {}

    /// Feed one line without its terminator; returns an event when a blank line completes one.
    public mutating func feed(line: String) -> ServerSentEvent? {
        if line.isEmpty {
            defer { name = "message"; data = [] }
            return data.isEmpty ? nil : ServerSentEvent(name: name, data: data.joined(separator: "\n"))
        }
        if line.hasPrefix(":") { return nil }
        let (field, value) = Self.split(line)
        switch field {
        case "event": name = value
        case "data": data.append(value)
        default: break
        }
        return nil
    }

    private static func split(_ line: String) -> (String, String) {
        guard let colon = line.firstIndex(of: ":") else { return (line, "") }
        var value = line[line.index(after: colon)...]
        if value.first == " " { value = value.dropFirst() }
        return (String(line[..<colon]), String(value))
    }
}

/// Snapshots pushed by the server. One connection; the caller decides when to reconnect.
public struct SnapshotStream: Sendable {
    private let request: URLRequest
    private let session: URLSession

    public init(request: URLRequest, session: URLSession = .shared) {
        self.request = request
        self.session = session
    }

    public func snapshots() -> AsyncThrowingStream<AssistantSnapshot, Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                do {
                    let (bytes, response) = try await session.bytes(for: request)
                    guard let http = response as? HTTPURLResponse, (200 ..< 300).contains(http.statusCode) else {
                        throw PoupleAPIError.invalidResponse
                    }
                    let decoder = SnapshotDecoder.make()
                    var parser = ServerSentEventParser()
                    // `AsyncBytes.lines` drops empty lines, which are the event delimiters: split on newlines ourselves.
                    var line: [UInt8] = []
                    for try await byte in bytes {
                        guard byte == 0x0A else { line.append(byte); continue }
                        if line.last == 0x0D { line.removeLast() }
                        let text = String(decoding: line, as: UTF8.self)
                        line.removeAll(keepingCapacity: true)
                        // The core names its events "snapshot"; an unnamed one is the same payload and must not be dropped silently.
                        if let event = parser.feed(line: text), event.name == "snapshot" || event.name == "message" {
                            continuation.yield(try decoder.decode(AssistantSnapshot.self, from: Data(event.data.utf8)))
                        }
                    }
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: error)
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }
}
