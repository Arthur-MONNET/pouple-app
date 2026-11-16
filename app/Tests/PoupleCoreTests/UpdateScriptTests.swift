import XCTest
@testable import PoupleCore

final class UpdateScriptTests: XCTestCase {
    private let messages = UpdateScript.Messages(quitting: "Quitting", fetching: "Fetching", building: "Building", done: "Done", notQuit: "Not quit", badOrigin: "Bad origin", pullFailed: "Pull failed", installFailed: "Install failed", close: "Press Return")

    private func record(_ root: String) -> Data { Data(#"{"sourceRoot":"\#(root)","commit":"abc1234","version":"0.1.0"}"#.utf8) }

    func testAnInstallRecordPointingAtARealCheckoutIsAccepted() {
        let result = UpdateScript.plan(recordData: record("/Users/a/pouple-app"), directoryExists: { $0 == "/Users/a/pouple-app" }, fileExists: { $0 == "/Users/a/pouple-app/scripts/install-local.sh" })
        XCTAssertEqual(try result.get().sourceRoot, "/Users/a/pouple-app")
    }

    func testWhatCannotBeTrustedIsRefused() {
        let yes: (String) -> Bool = { _ in true }
        XCTAssertEqual(UpdateScript.plan(recordData: nil, directoryExists: yes, fileExists: yes), .failure(.notInstalledFromSource))
        XCTAssertEqual(UpdateScript.plan(recordData: Data("nope".utf8), directoryExists: yes, fileExists: yes), .failure(.invalidRecord))
        for root in ["relative/path", "/a/../etc", "/a/b/..", "/tmp/x\\ny"] {
            XCTAssertEqual(UpdateScript.plan(recordData: record(root), directoryExists: yes, fileExists: yes), .failure(.invalidRecord), root)
        }
        XCTAssertEqual(UpdateScript.plan(recordData: record("/Users/a/gone"), directoryExists: { _ in false }, fileExists: yes), .failure(.checkoutMissing("/Users/a/gone")))
        XCTAssertEqual(UpdateScript.plan(recordData: record("/Users/a/x"), directoryExists: yes, fileExists: { _ in false }), .failure(.checkoutMissing("/Users/a/x")))
    }

    func testQuotingKeepsAnyPathAsOneHarmlessWord() {
        XCTAssertEqual(UpdateScript.shellQuote("/Users/a b/pouple-app"), "'/Users/a b/pouple-app'")
        XCTAssertEqual(UpdateScript.shellQuote("/Users/o'brien/x"), "'/Users/o'\\''brien/x'")
        let nasty = "/tmp/$(rm -rf ~)`id`;\"x\"&|<>"
        XCTAssertEqual(UpdateScript.shellQuote(nasty), "'\(nasty)'")
    }

    func testTheScriptDoesWhatItPromisesInOrder() {
        let text = UpdateScript.content(sourceRoot: "/Users/a b/pouple-app", branch: .main, appPID: 4242, messages: messages)
        XCTAssertTrue(text.hasPrefix("#!/bin/bash\n"))
        XCTAssertTrue(text.contains("ROOT='/Users/a b/pouple-app'"))
        XCTAssertTrue(text.contains("APP_PID=4242"))
        XCTAssertTrue(text.contains("BRANCH='main'"))
        let steps = ["kill -0 \"$APP_PID\"", "git remote get-url origin", "git fetch origin \"$BRANCH\"", "git switch \"$BRANCH\"", "git pull --ff-only origin \"$BRANCH\"", "bash scripts/install-local.sh"]
        var last = text.startIndex
        for step in steps {
            guard let range = text.range(of: step, range: last ..< text.endIndex) else { return XCTFail("missing or out of order: \(step)") }
            last = range.upperBound
        }
        XCTAssertTrue(text.contains("GIT_TERMINAL_PROMPT=0"))
        XCTAssertFalse(text.contains("sudo"))
    }

    func testTheBetaSwitchesTheCheckoutToDevelopBeforePulling() {
        let text = UpdateScript.content(sourceRoot: "/x", branch: .develop, appPID: 1, messages: messages)
        XCTAssertTrue(text.contains("BRANCH='develop'"))
        XCTAssertTrue(text.contains("git switch --create \"$BRANCH\" --track \"origin/$BRANCH\""), "a checkout that never had develop gets it from origin")
        XCTAssertFalse(text.contains("origin main"), "nothing of main is pulled for the beta")
    }

    func testAPageMessageCanOnlyNameMainOrDevelop() {
        XCTAssertEqual(UpdateScript.Branch(messageValue: nil), .main)
        XCTAssertEqual(UpdateScript.Branch(messageValue: "develop"), .develop)
        XCTAssertEqual(UpdateScript.Branch(messageValue: "main"), .main)
        for bad: Any in ["feat/x", "main; rm -rf ~", "", 3, ["develop"]] {
            XCTAssertNil(UpdateScript.Branch(messageValue: bad), "\(bad)")
        }
    }

    func testTheCoresStatusSaysWhetherTheUpdateCanStartFromTheMenu() throws {
        func decode(_ json: String) throws -> UpdateStatus { try JSONDecoder().decode(UpdateStatus.self, from: Data(json.utf8)) }
        let beta = try decode(#"{"channel":"beta","branch":"develop","supported":true,"reason":"","message":"","installed":{"version":"0.2.0+beta.3","commit":"abc","installedAt":"x"},"latest":{"commit":"def"},"available":true,"behind":2,"changes":["feat: a"],"blockers":[],"checkedAt":null,"checking":false,"error":null}"#)
        XCTAssertTrue(beta.isBeta)
        XCTAssertEqual(beta.branch, .develop)
        XCTAssertTrue(beta.canUpdateNow)
        let blocked = try decode(#"{"channel":"stable","branch":"main","supported":true,"available":true,"changes":[],"blockers":[{"code":"local-changes","message":"Commit first."}],"error":null}"#)
        XCTAssertFalse(blocked.canUpdateNow)
        let unknownBranch = try decode(#"{"channel":"nightly","branch":"nightly","supported":true,"available":true,"blockers":[]}"#)
        XCTAssertNil(unknownBranch.branch)
        XCTAssertFalse(unknownBranch.canUpdateNow, "a branch this build does not know is never put in the script")
    }

    func testOnlyTheProjectsOwnRepositoryIsAcceptedAsOrigin() {
        let text = UpdateScript.content(sourceRoot: "/x", branch: .main, appPID: 1, messages: messages)
        for origin in UpdateScript.allowedOrigins { XCTAssertTrue(text.contains("'\(origin)'")) }
        XCTAssertEqual(UpdateScript.allowedOrigins.count, 4)
        XCTAssertFalse(text.contains("*)  ;;"), "no wildcard accepts another origin")
        XCTAssertTrue(text.contains("*) fail 'Bad origin' ;;"))
    }

    func testTheScriptRemovesItselfOnlyAfterSuccess() {
        let text = UpdateScript.content(sourceRoot: "/x", branch: .main, appPID: 1, messages: messages)
        let remove = text.range(of: "rm -f -- \"$SELF\"")!
        XCTAssertGreaterThan(remove.lowerBound, text.range(of: "bash scripts/install-local.sh")!.upperBound)
        XCTAssertFalse(text.contains("rm -rf"), "never a recursive removal")
    }

    func testMessagesWithQuotesCannotBreakTheScript() {
        let tricky = UpdateScript.Messages(quitting: "it's", fetching: "a", building: "b", done: "c", notQuit: "d", badOrigin: "e", pullFailed: "f", installFailed: "g", close: "h")
        XCTAssertTrue(UpdateScript.content(sourceRoot: "/x", branch: .main, appPID: 1, messages: tricky).contains("say 'it'\\''s'"))
    }
}
