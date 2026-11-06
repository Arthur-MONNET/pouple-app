import Foundation

/// Defines `window.__POUPLE__` before the page runs so the core's HTML never carries the token.
public enum PageBootstrap {

    /// `token` as a JavaScript string literal that cannot end the literal, the script or the page early.
    static func javaScriptString(_ text: String) -> String {
        let json = (try? JSONEncoder().encode(text)).map { String(decoding: $0, as: UTF8.self) } ?? "\"\""
        // JSONEncoder escapes quotes, backslashes and control characters (and `/`); `<` `>` `&` and the two
        // line separators JavaScript treats as newlines are escaped here, so `</script>` can never appear.
        return json
            .replacingOccurrences(of: "<", with: "\\u003c")
            .replacingOccurrences(of: ">", with: "\\u003e")
            .replacingOccurrences(of: "&", with: "\\u0026")
            .replacingOccurrences(of: "\u{2028}", with: "\\u2028")
            .replacingOccurrences(of: "\u{2029}", with: "\\u2029")
    }

    /// The document-start script. It does nothing outside the core's own origin, whatever frame it lands in.
    public static func userScript(token: String, port: Int) -> String {
        "if (location.origin === \"http://127.0.0.1:\(port)\") { window.__POUPLE__ = { token: \(javaScriptString(token)) }; }"
    }

}
