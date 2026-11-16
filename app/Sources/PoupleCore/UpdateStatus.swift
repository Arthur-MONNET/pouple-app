import Foundation

/// What the core says about updates (POST /api/update/check): every text is already in the interface language.
public struct UpdateStatus: Decodable, Equatable, Sendable {
    public struct Installed: Decodable, Equatable, Sendable {
        public let version: String
    }

    public struct Blocker: Decodable, Equatable, Sendable {
        public let code: String
        public let message: String
    }

    /// "stable" or "beta".
    public let channel: String
    /// The branch the channel follows; nil when the core named one this build does not know.
    public let branch: UpdateScript.Branch?
    public let supported: Bool
    /// Why updates are not offered, when they are not supported.
    public let message: String
    public let installed: Installed?
    public let available: Bool
    public let changes: [String]
    public let blockers: [Blocker]
    /// Why the last check could not finish.
    public let error: String?

    public var isBeta: Bool { channel == "beta" }

    /// Whether the update can be started from here: there is one, and nothing in the source folder stops it.
    public var canUpdateNow: Bool { supported && available && blockers.isEmpty && branch != nil }

    enum CodingKeys: String, CodingKey {
        case channel, branch, supported, message, installed, available, changes, blockers, error
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        channel = try container.decodeIfPresent(String.self, forKey: .channel) ?? "stable"
        branch = try UpdateScript.Branch(rawValue: container.decodeIfPresent(String.self, forKey: .branch) ?? "main")
        supported = try container.decode(Bool.self, forKey: .supported)
        message = try container.decodeIfPresent(String.self, forKey: .message) ?? ""
        installed = try container.decodeIfPresent(Installed.self, forKey: .installed)
        available = try container.decodeIfPresent(Bool.self, forKey: .available) ?? false
        changes = try container.decodeIfPresent([String].self, forKey: .changes) ?? []
        blockers = try container.decodeIfPresent([Blocker].self, forKey: .blockers) ?? []
        error = try container.decodeIfPresent(String.self, forKey: .error)
    }
}
