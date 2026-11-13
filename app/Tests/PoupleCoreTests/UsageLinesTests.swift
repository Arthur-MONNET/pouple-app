import XCTest
@testable import PoupleCore

final class UsageLinesTests: XCTestCase {
    func testTheMenuShowsTheTokensOfTheDayThenEachAIsWindows() {
        let usage = AssistantUsage(
            today: 7, limit: 40, tokensToday: TokenCount(input: 412_000, cached: 351_000, output: 9_800),
            limits: [
                "codex": UsageLimits(fiveHour: UsageWindow(usedPercent: 9), week: UsageWindow(usedPercent: 82)),
                "claude": UsageLimits(week: UsageWindow(usedPercent: 60)),
            ]
        )
        XCTAssertEqual(UsageLines.lines(usage, language: .en), ["421.8K tokens today", "Claude · week: 60%", "Codex · 5 h: 9% · week: 82%"])
        let french = UsageLines.lines(usage, language: .fr)
        XCTAssertEqual(french.count, 3)
        XCTAssertTrue(french[0].hasPrefix("421,8"), french[0])
        XCTAssertTrue(french[2].contains("semaine\u{00A0}: 82"), french[2])
    }

    func testAWindowThatStartedAgainIsLeftOut() {
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        let usage = AssistantUsage(today: 1, limits: ["codex": UsageLimits(
            fiveHour: UsageWindow(usedPercent: 90, resetsAt: now.addingTimeInterval(-60)),
            week: UsageWindow(usedPercent: 30, resetsAt: now.addingTimeInterval(3600))
        )])
        XCTAssertEqual(UsageLines.lines(usage, language: .en, now: now), ["Codex · week: 30%"])
    }

    func testNothingIsShownWithoutUsage() {
        XCTAssertEqual(UsageLines.lines(nil, language: .en), [])
        XCTAssertEqual(UsageLines.lines(AssistantUsage(today: 0, tokensToday: TokenCount(input: 0, cached: 0, output: 0), limits: ["codex": UsageLimits()]), language: .en), [])
    }

    func testTheUsageOfTheSnapshotIsReadAndOptional() throws {
        let json = ModelsTests.snapshotJSON.replacingOccurrences(
            of: #""notices": ["Group chats: missing scope"] }"#,
            with: #""notices": ["Group chats: missing scope"], "usage": { "today": 3, "limit": null, "tokensToday": { "input": 10, "cached": 4, "output": 2 }, "limits": { "codex": { "fiveHour": { "usedPercent": 9.5, "resetsAt": "2026-11-13T13:49:05.000Z" }, "seenAt": "2026-11-13T12:00:00.000Z" } } } }"#
        )
        let usage = try XCTUnwrap(SnapshotDecoder.make().decode(AssistantSnapshot.self, from: Data(json.utf8)).status.usage)
        XCTAssertEqual(usage.tokensToday?.total, 12)
        XCTAssertEqual(usage.limits?["codex"]?.fiveHour?.usedPercent, 9.5)
        XCTAssertNotNil(usage.limits?["codex"]?.fiveHour?.resetsAt)
        XCTAssertNil(try SnapshotDecoder.make().decode(AssistantSnapshot.self, from: Data(ModelsTests.snapshotJSON.utf8)).status.usage)
    }
}
