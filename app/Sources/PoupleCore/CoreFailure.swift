import Foundation

/// Why the core is not running. Kept as data, not text, so the window can show it in the current language.
public enum CoreFailure: Equatable, Sendable {
    case stopped(status: Int32?)
    case closedBeforeReady
    case timeout(seconds: Int)
    case binaryMissing(path: String)
    case readyLine(ReadyLine.Failure)
    /// A message that is already text (a system error), shown as it is.
    case other(String)

    public func message(language: Language? = nil) -> String {
        let language = language ?? L10n.current
        switch self {
        case let .stopped(status):
            let code = status.map(String.init) ?? L10n.text(.failStatusUnknown, language: language)
            return L10n.text(.failStopped, code, language: language)
        case .closedBeforeReady:
            return L10n.text(.failClosedBeforeReady, language: language)
        case let .timeout(seconds):
            return L10n.plural(one: .failTimeoutOne, other: .failTimeoutOther, count: seconds, language: language)
        case let .binaryMissing(path):
            return L10n.text(.failBinaryMissing, path, language: language)
        case let .readyLine(failure):
            switch failure {
            case .malformed: return L10n.text(.failReadyMalformed, language: language)
            case .notReady: return L10n.text(.failNotReady, language: language)
            case let .invalidPort(port): return L10n.text(.failInvalidPort, port, language: language)
            }
        case let .other(text):
            return text
        }
    }
}
