import XCTest
@testable import PoupleCore

final class PageBootstrapTests: XCTestCase {
    func testTheScriptDefinesTheTokenOnlyForTheCoresOrigin() {
        let script = PageBootstrap.userScript(token: "abc123", port: 5123)
        XCTAssertEqual(script, #"if (location.origin === "http://127.0.0.1:5123") { window.__POUPLE__ = { token: "abc123" }; }"#)
    }

    func testQuotesBackslashesAndScriptEndsCannotBreakOutOfTheLiteral() {
        let nasty = "a\"b\\c</script><!--&\u{2028}\n'x"
        let literal = PageBootstrap.javaScriptString(nasty)
        XCTAssertFalse(literal.lowercased().contains("</script"))
        XCTAssertFalse(literal.contains("<"))
        XCTAssertFalse(literal.contains("\n"))
        XCTAssertFalse(literal.contains("\u{2028}"))
        // Decoding the literal as JSON gives the original text back: nothing was lost or reinterpreted.
        let decoded = try? JSONDecoder().decode(String.self, from: Data(literal.utf8))
        XCTAssertEqual(decoded, nasty)
        // A quote or backslash is always escaped, so the literal has exactly its two delimiters.
        let unescapedQuotes = literal.dropFirst().dropLast().replacingOccurrences(of: "\\\\", with: "").replacingOccurrences(of: "\\\"", with: "")
        XCTAssertFalse(unescapedQuotes.contains("\""))
    }

    func testAnOrdinaryHexTokenIsLeftAlone() {
        let token = String(repeating: "ab12", count: 16)
        XCTAssertEqual(PageBootstrap.javaScriptString(token), "\"\(token)\"")
    }

    func testTheCoreReceivesTheTokenOverItsLifetimePipe() async throws {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("pouple-test-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: dir) }
        let script = dir.appendingPathComponent("fake-core")
        let token = String(repeating: "ab12", count: 16)
        try """
        #!/bin/sh
        IFS= read -r token
        if [ "$POUPLE_SHELL_INJECTS_TOKEN" = "1" ] && [ -z "${POUPLE_TOKEN+x}" ] && [ "$token" = "\(token)" ] && [ "$POUPLE_PORT" = "0" ]; then
          echo '{"ready":true,"port":4321}'
        else
          echo '{"ready":false}'
        fi
        cat > /dev/null
        """.write(to: script, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: script.path)

        let process = try ProcessCoreLauncher(executable: script, environment: ["POUPLE_TOKEN": "inherited"]).launch(token: token)
        var iterator = process.lines.makeAsyncIterator()
        let line = await iterator.next()
        XCTAssertEqual(ReadyLine.parse(line ?? ""), .success(4321))
        process.closeInput()
        let status = await process.waitForExit()
        XCTAssertEqual(status, 0)
    }
}
