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
    case pipeline
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

    /// The AI is still working on it; the user has nothing to do yet.
    public var isInProgress: Bool {
        switch self {
        case .queued, .analyzing, .applying: return true
        case .awaitingUser, .ready, .done, .ignored, .failed, .unknown: return false
        }
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
    /// Absent from older cores; read it through `allowsSeveral`.
    public let multiple: Bool?

    public init(id: String, text: String, options: [AssistantOption], multiple: Bool? = nil) {
        self.id = id
        self.text = text
        self.options = options
        self.multiple = multiple
    }

    /// Several options can be picked together (several actions to take, for example); otherwise one.
    public var allowsSeveral: Bool { multiple == true }
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
    /// Set by the core once the request has reached the user; absent from older cores. Read `isEngaged`.
    public var engaged: Bool? = nil
    /// Set by the core when the user closed the bubble; absent from older cores. Read `isClosed`.
    public var closed: Bool? = nil
    /// The other AI ("codex", "claude") that can take over a request stopped by a usage limit; absent otherwise.
    public var switchTo: String? = nil

    /// The name of the AI that can take over, as shown in a button.
    public var switchToName: String? {
        switchTo.map { $0 == "claude" ? "Claude" : $0 == "codex" ? "Codex" : $0 }
    }

    /// The request has reached the user (a question, an answer, a failure, a change made): from then on the work on it
    /// shows in its bubble. Before, the work is silent.
    public var isEngaged: Bool { engaged == true }
    /// The user closed the bubble; it comes back the next time the request needs them.
    public var isClosed: Bool { closed == true }

    /// "Request from Morgan in #front", "Note to self"... as in the web app.
    public var title: String { title(language: nil) }

    public func title(language: Language?) -> String {
        let channel = "#\(source.channelName ?? source.channelId)"
        switch source.origin {
        case .poupleAction: return source.label ?? L10n.text(.titleAction, language: language)
        case .pipeline: return text.split(separator: "\n", maxSplits: 1).first.map(String.init) ?? source.senderName
        case .noteToSelf: return L10n.text(.titleNoteToSelf, language: language)
        case .directMessage: return L10n.text(.titleDirect, source.senderName, language: language)
        case .groupMessage: return L10n.text(.titleGroup, source.senderName, language: language)
        case .broadcast: return L10n.text(.titleBroadcast, source.senderName, channel, language: language)
        case .keyword: return L10n.text(.titleKeyword, source.senderName, channel, language: language)
        case .threadReply: return L10n.text(.titleThread, source.senderName, channel, language: language)
        case .channelMessage, .mention, .unknown: return L10n.text(.titleChannel, source.senderName, channel, language: language)
        }
    }
}

public struct AssistantStatusInfo: Codable, Equatable, Sendable {
    public let active: Bool
    public let problem: String?
    public let notices: [String]
    /// Absent from older cores.
    public let usage: AssistantUsage?
}

/// Tokens an AI used: input (cached ones included), cached, output.
public struct TokenCount: Codable, Equatable, Sendable {
    public let input: Int64
    public let cached: Int64
    public let output: Int64

    public init(input: Int64, cached: Int64, output: Int64) {
        self.input = input
        self.cached = cached
        self.output = output
    }

    /// Everything read and written.
    public var total: Int64 { input + output }
}

/// One usage window of an AI subscription, as the AI last reported it.
public struct UsageWindow: Codable, Equatable, Sendable {
    /// From 0 to 100.
    public let usedPercent: Double
    public let resetsAt: Date?

    public init(usedPercent: Double, resetsAt: Date? = nil) {
        self.usedPercent = usedPercent
        self.resetsAt = resetsAt
    }
}

/// An AI's own usage windows: the current five hours and the current week.
public struct UsageLimits: Codable, Equatable, Sendable {
    public let fiveHour: UsageWindow?
    public let week: UsageWindow?

    public init(fiveHour: UsageWindow? = nil, week: UsageWindow? = nil) {
        self.fiveHour = fiveHour
        self.week = week
    }
}

public struct AssistantUsage: Codable, Equatable, Sendable {
    public let today: Int
    public let limit: Int?
    /// What each AI used today, by AI ("codex", "claude"). Never added up: each AI has its own plan.
    public let tokensByAI: [String: TokenCount]?
    /// By AI; an AI that never reported its windows is absent.
    public let limits: [String: UsageLimits]?

    public init(today: Int, limit: Int? = nil, tokensByAI: [String: TokenCount]? = nil, limits: [String: UsageLimits]? = nil) {
        self.today = today
        self.limit = limit
        self.tokensByAI = tokensByAI
        self.limits = limits
    }
}

public struct AssistantSnapshot: Codable, Equatable, Sendable {
    public let status: AssistantStatusInfo
    public let requests: [AssistantRequest]

    public init(status: AssistantStatusInfo, requests: [AssistantRequest]) {
        self.status = status
        self.requests = requests
    }

    /// At least one request is being worked on.
    public var isWorking: Bool { requests.contains { $0.status.isInProgress } }
}

public struct QuestionAnswer: Codable, Equatable, Sendable {
    public let questionId: String
    public let optionId: String?
    /// The options picked for a question that allows several.
    public let optionIds: [String]?
    public let text: String?

    public init(questionId: String, optionId: String? = nil, optionIds: [String]? = nil, text: String? = nil) {
        self.questionId = questionId
        self.optionId = optionId
        self.optionIds = optionIds
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
