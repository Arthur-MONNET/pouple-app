import XCTest
@testable import PoupleCore

final class ReadyLineTests: XCTestCase {
    func testTheCoresReadyLineGivesItsPort() {
        XCTAssertEqual(ReadyLine.parse(#"{"ready":true,"port":51234}"#), .success(51234))
        XCTAssertEqual(ReadyLine.parse("  {\"port\":80,\"ready\":true,\"extra\":1}\r\n"), .success(80))
    }

    func testAnythingElseIsRefusedWithAReason() {
        XCTAssertEqual(ReadyLine.parse("2026/11/06 listening"), .failure(.malformed))
        XCTAssertEqual(ReadyLine.parse(""), .failure(.malformed))
        XCTAssertEqual(ReadyLine.parse(#"{"ready":false,"port":5}"#), .failure(.notReady))
        XCTAssertEqual(ReadyLine.parse(#"{"port":5}"#), .failure(.notReady))
        XCTAssertEqual(ReadyLine.parse(#"{"ready":true}"#), .failure(.notReady))
        XCTAssertEqual(ReadyLine.parse(#"{"ready":true,"port":0}"#), .failure(.invalidPort(0)))
        XCTAssertEqual(ReadyLine.parse(#"{"ready":true,"port":70000}"#), .failure(.invalidPort(70000)))
        XCTAssertEqual(ReadyLine.parse(#"{"ready":true,"port":"80"}"#), .failure(.malformed))
    }
}

final class LineSplitterTests: XCTestCase {
    func testLinesAreCutAtNewlinesAcrossChunks() {
        var splitter = LineSplitter()
        XCTAssertEqual(splitter.feed(Data("{\"rea".utf8)), [])
        XCTAssertEqual(splitter.feed(Data("dy\":true}\r\nsecond\nthi".utf8)), ["{\"ready\":true}", "second"])
        XCTAssertEqual(splitter.finish(), "thi")
        XCTAssertNil(splitter.finish())
    }
}

final class BackoffTests: XCTestCase {
    func testTheDelayDoublesThenStopsAtTheCap() {
        let backoff = Backoff()
        let seconds = (0 ... 8).map { backoff.delay(afterFailures: $0).components.seconds }
        XCTAssertEqual(seconds, [0, 1, 2, 4, 8, 16, 30, 30, 30])
        XCTAssertEqual(backoff.delay(afterFailures: 10_000), .seconds(30))
    }
}

final class NavigationPolicyTests: XCTestCase {
    private let policy = NavigationPolicy(port: 5123)

    private func decision(_ text: String) -> NavigationDecision { policy.decision(for: URL(string: text)) }

    func testOnlyTheCoreOriginLoadsInTheWindow() {
        XCTAssertEqual(decision("http://127.0.0.1:5123/"), .allow)
        XCTAssertEqual(decision("http://127.0.0.1:5123/conversations/3?x=1"), .allow)
        XCTAssertEqual(decision("about:blank"), .allow)
    }

    func testEveryOtherWebLinkGoesToTheBrowser() {
        XCTAssertEqual(decision("https://slack.com/apps"), .openExternally)
        XCTAssertEqual(decision("http://127.0.0.1:9999/"), .openExternally)
        XCTAssertEqual(decision("http://localhost:5123/"), .openExternally)
        XCTAssertEqual(decision("https://127.0.0.1:5123/"), .openExternally)
        XCTAssertEqual(decision("http://127.0.0.1.evil.com:5123/"), .openExternally)
        XCTAssertEqual(decision("mailto:a@b.c"), .openExternally)
    }

    func testFilesAndOddSchemesAreDenied() {
        XCTAssertEqual(decision("file:///etc/passwd"), .deny)
        XCTAssertEqual(decision("javascript:alert(1)"), .deny)
        XCTAssertEqual(decision("data:text/html,hi"), .deny)
        XCTAssertEqual(decision("pouple://run"), .deny)
        XCTAssertEqual(decision("about:srcdoc"), .deny)
        XCTAssertEqual(policy.decision(for: nil), .deny)
    }
}

final class StatusSummaryTests: XCTestCase {
    func testTheMenuLineFollowsTheCoreAndTheSetup() {
        XCTAssertEqual(StatusSummary.line(core: .starting, setupComplete: nil), "Starting…")
        XCTAssertEqual(StatusSummary.line(core: .failed("x"), setupComplete: true), "Core unavailable")
        XCTAssertEqual(StatusSummary.line(core: .ready(port: 1), setupComplete: nil), "Checking…")
        XCTAssertEqual(StatusSummary.line(core: .ready(port: 1), setupComplete: false), "Setup needed")
        XCTAssertEqual(StatusSummary.line(core: .ready(port: 1), setupComplete: true), "Connected")
    }

    func testLoginItemStatesExplainThemselves() {
        XCTAssertTrue(LoginItemState.enabled.isOn)
        XCTAssertTrue(LoginItemState.needsApproval.isOn)
        XCTAssertFalse(LoginItemState.disabled.isOn)
        XCTAssertNotNil(LoginItemState.needsApproval.detail)
        XCTAssertNil(LoginItemState.enabled.detail)
    }
}

final class CoreLocatorTests: XCTestCase {
    func testTheEnvironmentOverridesTheBundle() {
        let bundle = URL(fileURLWithPath: "/Applications/Pouple.app/Contents/Resources")
        XCTAssertEqual(CoreLocator.locate(environment: [:], resourceURL: bundle).path, "/Applications/Pouple.app/Contents/Resources/pouple-core")
        XCTAssertEqual(CoreLocator.locate(environment: ["POUPLE_CORE_PATH": "/tmp/core"], resourceURL: bundle).path, "/tmp/core")
        XCTAssertEqual(CoreLocator.locate(environment: ["POUPLE_CORE_PATH": ""], resourceURL: bundle).lastPathComponent, "pouple-core")
    }

    func testAMissingBinaryIsReportedBeforeAnythingStarts() {
        let launcher = ProcessCoreLauncher(executable: URL(fileURLWithPath: "/nonexistent/pouple-core"))
        XCTAssertThrowsError(try launcher.launch(token: "t")) {
            XCTAssertEqual($0 as? CoreLaunchError, .binaryMissing("/nonexistent/pouple-core"))
        }
    }

    func testTokensAreRandom32ByteHex() {
        let first = CoreSupervisor.randomToken()
        XCTAssertEqual(first.count, 64)
        XCTAssertNotNil(first.range(of: "^[0-9a-f]{64}$", options: .regularExpression))
        XCTAssertNotEqual(first, CoreSupervisor.randomToken())
    }
}

@MainActor
final class SetupMonitorTests: XCTestCase {
    func testItPublishesTheAnswerAndForgetsItWhenDetached() async throws {
        let monitor = SetupMonitor(interval: .milliseconds(10))
        XCTAssertNil(monitor.setupComplete)
        monitor.attach { SetupState(setupComplete: true) }
        try await waitUntil { monitor.setupComplete == true }
        monitor.attach(fetch: nil)
        XCTAssertNil(monitor.setupComplete)
    }

    func testAFailedCallKeepsTheLastAnswer() async throws {
        struct Down: Error {}
        let calls = LockedBox(0)
        let monitor = SetupMonitor(interval: .milliseconds(10))
        monitor.attach {
            if calls.mutate({ $0 += 1; return $0 }) == 1 { return SetupState(setupComplete: false) }
            throw Down()
        }
        try await waitUntil { calls.mutate { $0 } >= 3 }
        XCTAssertEqual(monitor.setupComplete, false)
        monitor.stop()
    }
}

struct WaitTimeout: Error, CustomStringConvertible {
    let description = "Timed out waiting for a condition"
}

@MainActor
func waitUntil(timeout: TimeInterval = 3, _ condition: () -> Bool) async throws {
    let deadline = Date().addingTimeInterval(timeout)
    while !condition() {
        if Date() > deadline { throw WaitTimeout() }
        try await Task.sleep(for: .milliseconds(5))
    }
}
