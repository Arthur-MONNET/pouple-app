import XCTest
@testable import PoupleCore

final class UsageLinesTests: XCTestCase {
    func testEachAIHasItsOwnLineWithItsOwnTokens() {
        let usage = AssistantUsage(
            today: 44, limit: 150, tokensByAI: ["codex": TokenCount(input: 1_579_857, cached: 1_265_408, output: 8_132)],
            limits: [
                "codex": UsageLimits(fiveHour: UsageWindow(usedPercent: 13)),
                "claude": UsageLimits(fiveHour: UsageWindow(usedPercent: 92), week: UsageWindow(usedPercent: 64)),
            ]
        )
        // Claude used no tokens today: the ones of Codex are never shown as its.
        XCTAssertEqual(UsageLines.lines(usage, language: .en), ["Claude · 5 h: 92% · week: 64%", "Codex · 1.6M tokens · 5 h: 13%"])
        let french = UsageLines.lines(usage, language: .fr)
        XCTAssertTrue(french[0].contains("semaine\u{00A0}: 64"), french[0])
        XCTAssertTrue(french[1].contains("1,6"), french[1])
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
        XCTAssertEqual(UsageLines.lines(AssistantUsage(today: 0, tokensByAI: ["codex": TokenCount(input: 0, cached: 0, output: 0)], limits: ["codex": UsageLimits()]), language: .en), [])
    }

    func testTheUsageOfTheSnapshotIsReadAndOptional() throws {
        let json = ModelsTests.snapshotJSON.replacingOccurrences(
            of: #""notices": ["Group chats: missing scope"] }"#,
            with: #""notices": ["Group chats: missing scope"], "usage": { "today": 3, "limit": null, "tokensByAI": { "codex": { "input": 10, "cached": 4, "output": 2 } }, "limits": { "codex": { "fiveHour": { "usedPercent": 9.5, "resetsAt": "2026-11-13T13:49:05.000Z" }, "seenAt": "2026-11-13T12:00:00.000Z" } } } }"#
        )
        let usage = try XCTUnwrap(SnapshotDecoder.make().decode(AssistantSnapshot.self, from: Data(json.utf8)).status.usage)
        XCTAssertEqual(usage.tokensByAI?["codex"]?.total, 12)
        XCTAssertEqual(usage.limits?["codex"]?.fiveHour?.usedPercent, 9.5)
        XCTAssertNotNil(usage.limits?["codex"]?.fiveHour?.resetsAt)
        XCTAssertNil(try SnapshotDecoder.make().decode(AssistantSnapshot.self, from: Data(ModelsTests.snapshotJSON.utf8)).status.usage)
    }
}
