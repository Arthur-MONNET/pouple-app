import Foundation

public enum PoupleAPIError: Error, Equatable, LocalizedError {
    case http(status: Int, message: String)
    case invalidResponse

    public var errorDescription: String? {
        switch self {
        case let .http(status, message): return message.isEmpty ? L10n.text(.apiAnswered, status) : message
        case .invalidResponse: return L10n.text(.apiUnexpected)
        }
    }
}

/// Where the core listens and the token that proves a call comes from the app.
public struct CoreConnection: Equatable, Sendable {
    public let baseURL: URL
    public let token: String

    public init(port: Int, token: String) {
        // The core only accepts this exact Host, so never `localhost`.
        self.baseURL = URL(string: "http://127.0.0.1:\(port)")!
        self.token = token
    }

    public init(baseURL: URL, token: String) {
        self.baseURL = baseURL
        self.token = token
    }
}

/// What `GET /api/state` says about the setup.
public struct SetupState: Codable, Equatable, Sendable {
    public struct Step: Codable, Equatable, Sendable {
        public let id: String
        public let name: String
        public let required: Bool
        public let connected: Bool
    }

    public let setupComplete: Bool
    public let steps: [Step]
    /// The interface language the core is set to; nil when it does not say (or says something unknown).
    public let language: Language?

    public init(setupComplete: Bool, steps: [Step] = [], language: Language? = nil) {
        self.setupComplete = setupComplete
        self.steps = steps
        self.language = language
    }

    private enum CodingKeys: String, CodingKey { case setupComplete, steps, language }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        setupComplete = try container.decode(Bool.self, forKey: .setupComplete)
        steps = try container.decodeIfPresent([Step].self, forKey: .steps) ?? []
        language = (try? container.decodeIfPresent(String.self, forKey: .language)).flatMap { $0 }.flatMap(Language.init(code:))
    }
}

/// The core's HTTP API: setup state and the assistant endpoints. Every call carries the bearer token in a header,
/// never in the URL.
public struct PoupleAPI: Sendable {
    public let connection: CoreConnection
    private let session: URLSession

    public var baseURL: URL { connection.baseURL }

    public init(connection: CoreConnection, session: URLSession = .shared) {
        self.connection = connection
        self.session = session
    }

    public func setupState() async throws -> SetupState {
        let data = try await send("GET", "/api/state")
        return try JSONDecoder().decode(SetupState.self, from: data)
    }

    /// Switches the interface language of the whole app (the core stores it, the web UI follows).
    public func setUILanguage(_ language: Language) async throws {
        _ = try await send("PUT", "/api/settings", body: ["uiLanguage": language.rawValue])
    }

    public func snapshot() async throws -> AssistantSnapshot {
        let data = try await send("GET", "/api/assistant")
        return try SnapshotDecoder.make().decode(AssistantSnapshot.self, from: data)
    }

    public func answer(requestId: String, answers: [QuestionAnswer]) async throws {
        _ = try await send("POST", "/api/assistant/requests/\(requestId)/answers", body: ["answers": answers.map(Self.dictionary)])
    }

    public func decideProposal(requestId: String, approve: Bool) async throws {
        _ = try await send("POST", "/api/assistant/requests/\(requestId)/proposal", body: ["decision": approve ? "approve" : "decline"])
    }

    public func message(requestId: String, text: String) async throws {
        _ = try await send("POST", "/api/assistant/requests/\(requestId)/messages", body: ["text": text])
    }

    public func ignore(requestId: String) async throws {
        _ = try await send("POST", "/api/assistant/requests/\(requestId)/ignore")
    }

    /// Hides the request's bubble until it needs the user again; the conversation is kept. `seen` is the update time of
    /// the request the user closed: the core refuses with 409 when the request changed since, so a newer state stays open.
    public func close(requestId: String, seen: Date) async throws {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        _ = try await send("POST", "/api/assistant/requests/\(requestId)/close", body: ["updatedAt": formatter.string(from: seen)])
    }

    public func retry(requestId: String) async throws {
        _ = try await send("POST", "/api/assistant/requests/\(requestId)/retry")
    }

    /// Stops the Codex turn running for this request.
    public func stop(requestId: String) async throws {
        _ = try await send("POST", "/api/assistant/requests/\(requestId)/stop")
    }

    public func delete(requestId: String) async throws {
        _ = try await send("DELETE", "/api/assistant/requests/\(requestId)")
    }

    /// The server-sent-events request for snapshots, authorized like every other call.
    public func streamRequest() -> URLRequest {
        var request = URLRequest(url: baseURL.appendingPathComponent("api/assistant/stream"))
        request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
        request.setValue("Bearer \(connection.token)", forHTTPHeaderField: "Authorization")
        request.timeoutInterval = .infinity
        return request
    }

    private static func dictionary(_ answer: QuestionAnswer) -> [String: Any] {
        var result: [String: Any] = ["questionId": answer.questionId]
        if let optionId = answer.optionId { result["optionId"] = optionId }
        if let optionIds = answer.optionIds { result["optionIds"] = optionIds }
        if let text = answer.text { result["text"] = text }
        return result
    }

    private func send(_ method: String, _ path: String, body: [String: Any]? = nil) async throws -> Data {
        var request = URLRequest(url: baseURL.appendingPathComponent(path))
        request.httpMethod = method
        request.timeoutInterval = 15
        request.setValue("Bearer \(connection.token)", forHTTPHeaderField: "Authorization")
        if let body {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = try JSONSerialization.data(withJSONObject: body)
        }
        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw PoupleAPIError.invalidResponse }
        guard (200 ..< 300).contains(http.statusCode) else {
            let message = (try? JSONSerialization.jsonObject(with: data) as? [String: Any])?["error"] as? String
            throw PoupleAPIError.http(status: http.statusCode, message: message ?? "")
        }
        return data
    }
}
