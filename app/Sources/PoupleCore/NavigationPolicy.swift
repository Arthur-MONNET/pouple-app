import Foundation

/// What the main window does with a URL it is asked to load.
public enum NavigationDecision: Equatable, Sendable {
    /// The core's own page: loaded in the window.
    case allow
    /// Any other web or mail link: handed to the default browser (or mail client).
    case openExternally
    /// Anything else (file:, data:, javascript:, custom schemes): refused.
    case deny
}

/// Only the core's origin (`http://127.0.0.1:PORT`) is shown in the window. The page carries the API token, so no
/// other origin may ever be rendered in the same web view.
public struct NavigationPolicy: Equatable, Sendable {
    public let port: Int

    public init(port: Int) {
        self.port = port
    }

    public func decision(for url: URL?) -> NavigationDecision {
        guard let url, let scheme = url.scheme?.lowercased() else { return .deny }
        switch scheme {
        case "http", "https":
            if scheme == "http", url.host?.lowercased() == "127.0.0.1", url.port == port { return .allow }
            return .openExternally
        case "mailto":
            return .openExternally
        case "about":
            // The blank page a web view starts from.
            return url.absoluteString == "about:blank" ? .allow : .deny
        default:
            return .deny
        }
    }
}
