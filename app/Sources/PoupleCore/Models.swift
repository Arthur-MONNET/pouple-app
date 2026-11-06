import Foundation

// Mirrors shared/assistant.ts. Unknown values decode to `.unknown` so a newer server never breaks the overlay.

public enum RequestOrigin: String, Codable, Sendable {
    case mention
    case directMessage = "direct-message"
    case noteToSelf = "note-to-self"
    case groupMessage = "group-message"
    case broadcast
    case keyword
    case threadReply = "thread-reply"
    case channelMessage = "channel-message"
    case poupleAction = "pouple-action"
    case unknown

    public init(from decoder: Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        self = RequestOrigin(rawValue: raw) ?? .unknown
    }
}

public enum RequestStatus: String, Codable, Sendable {
    case queued
    case analyzing
    case awaitingUser = "awaiting-user"
    case ready
    case applying
    case done
    case ignored
    case failed
    case unknown

    public init(from decoder: Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        self = RequestStatus(rawValue: raw) ?? .unknown
    }
}

public struct RequestSource: Codable, Equatable, Sendable {
    public let origin: RequestOrigin
    public let channelId: String
    public let channelName: String?
    public let senderId: String
    public let senderName: String
    public let ts: String
    public let permalink: String?
    /// Title of an action started from Pouple ("Run vision on iOS").
    public let label: String?
}

public struct AssistantOption: Codable, Equatable, Identifiable, Sendable {
    public let id: String
    public let label: String
    public let description: String?

    public init(id: String, label: String, description: String? = nil) {
        self.id = id
        self.label = label
        self.description = description
    }
}

public struct AssistantQuestion: Codable, Equatable, Identifiable, Sendable {
    public let id: String
    public let text: String
    public let options: [AssistantOption]

    public init(id: String, text: String, options: [AssistantOption]) {
        self.id = id
        self.text = text
        self.options = options
    }
}

public struct AssistantProposal: Codable, Equatable, Sendable {
    public let title: String
    public let description: String
    public let projectId: String
}

public struct ChatEntry: Codable, Equatable, Identifiable, Sendable {
    public enum Role: String, Codable, Sendable { case system, assistant, user }
    public enum Kind: String, Codable, Sendable { case text, step }

    public let id: String
    public let role: Role
    public let kind: Kind
    public let text: String
}

public struct AssistantRequest: Codable, Equatable, Identifiable, Sendable {
    public let id: String
    public let source: RequestSource
    public let text: String
    public let receivedAt: Date
    public let updatedAt: Date
    public let status: RequestStatus
    public let summary: String?
    public let draftReply: String?
    public let questions: [AssistantQuestion]
    public let proposal: AssistantProposal?
    public let entries: [ChatEntry]
    public let error: String?

    /// "Request from Morgan in #front", "Note to self"... as in the web app.
    public var title: String {
        let channel = "#\(source.channelName ?? source.channelId)"
        switch source.origin {
        case .poupleAction: return source.label ?? "Pouple action"
        case .noteToSelf: return "Note to self"
        case .directMessage: return "\(source.senderName) · direct message"
        case .groupMessage: return "\(source.senderName) · group chat"
        case .broadcast: return "\(source.senderName) · @here in \(channel)"
        case .keyword: return "\(source.senderName) · watched word in \(channel)"
        case .threadReply: return "\(source.senderName) · thread in \(channel)"
        case .channelMessage, .mention, .unknown: return "\(source.senderName) · \(channel)"
        }
    }
}

public struct AssistantStatusInfo: Codable, Equatable, Sendable {
    public let active: Bool
    public let problem: String?
    public let notices: [String]
}

public struct AssistantSnapshot: Codable, Equatable, Sendable {
    public let status: AssistantStatusInfo
    public let requests: [AssistantRequest]

    public init(status: AssistantStatusInfo, requests: [AssistantRequest]) {
        self.status = status
        self.requests = requests
    }
}

public struct QuestionAnswer: Codable, Equatable, Sendable {
    public let questionId: String
    public let optionId: String?
    public let text: String?

    public init(questionId: String, optionId: String? = nil, text: String? = nil) {
        self.questionId = questionId
        self.optionId = optionId
        self.text = text
    }
}

public enum SnapshotDecoder {
    /// The server writes ISO 8601 dates, with milliseconds.
    public static func make() -> JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .custom { decoder in
            let text = try decoder.singleValueContainer().decode(String.self)
            let withFraction = ISO8601DateFormatter()
            withFraction.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            let plain = ISO8601DateFormatter()
            plain.formatOptions = [.withInternetDateTime]
            if let date = withFraction.date(from: text) ?? plain.date(from: text) { return date }
            throw DecodingError.dataCorrupted(.init(codingPath: decoder.codingPath, debugDescription: "Not an ISO 8601 date: \(text)"))
        }
        return decoder
    }
}
