import Foundation

/// A file stored by the core, waiting for the message that sends it.
public struct AttachmentRef: Codable, Equatable, Identifiable, Sendable {
    public let id: String
    public let name: String
    public let mime: String
    public let size: Int64
    /// "image" (shown to the AI as an image), "text" (read as text) or "other".
    public let kind: String

    public init(id: String, name: String, mime: String, size: Int64, kind: String) {
        self.id = id
        self.name = name
        self.mime = mime
        self.size = size
        self.kind = kind
    }

    public var isImage: Bool { kind == "image" }
}

/// Why a file cannot be added to a message.
public enum AttachmentProblem: Equatable, Sendable {
    case tooMany
    case tooLarge
    case empty
    case unsupported
}

/// What a message may carry, the same as in the web window: a few images and text files, each of a reasonable size.
public enum AttachmentRules {
    public static let maxFiles = 6
    public static let maxBytes: Int64 = 8 * 1024 * 1024

    static let imageExtensions: Set<String> = ["png", "jpg", "jpeg", "gif", "webp"]
    static let textExtensions: Set<String> = ["txt", "md", "json", "log", "csv", "yml", "yaml", "xml"]

    /// The reason a file cannot be added, or nil when it can. The core looks at the bytes to tell what a file really is;
    /// here the extension only decides what is worth sending.
    public static func problem(name: String, size: Int64, alreadyAdded: Int) -> AttachmentProblem? {
        if alreadyAdded >= maxFiles { return .tooMany }
        if size <= 0 { return .empty }
        if size > maxBytes { return .tooLarge }
        let ext = (name as NSString).pathExtension.lowercased()
        return imageExtensions.contains(ext) || textExtensions.contains(ext) ? nil : .unsupported
    }
}
