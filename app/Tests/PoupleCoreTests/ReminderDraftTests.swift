import XCTest
@testable import PoupleCore

final class ReminderDraftTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func offer(_ moments: [TimeInterval]) throws -> ReminderOffer {
        let items = moments.enumerated().map { index, offset in
            #"{ "id": "r\#(index + 1)", "label": "Reminder \#(index + 1)", "note": null, "at": "\#(ISO8601DateFormatter().string(from: now.addingTimeInterval(offset)))" }"#
        }
        let json = #"{ "id": "o1", "status": "proposed", "items": [\#(items.joined(separator: ","))] }"#
        return try SnapshotDecoder.make().decode(ReminderOffer.self, from: Data(json.utf8))
    }

    func testNothingIsTickedAtFirstAndEachOptionStartsAtTheProposedMoment() throws {
        let draft = ReminderDraft(offer: try offer([3600, 7200]))
        XCTAssertTrue(draft.chosen.isEmpty)
        XCTAssertFalse(draft.canConfirm(now: now))
        XCTAssertNil(draft.picks(now: now))
        XCTAssertEqual(draft.time(of: "r2"), now.addingTimeInterval(7200))
    }

    func testTheTickedOptionsAreSentInTheOrderTheyAreListedAtTheirOwnMoments() throws {
        var draft = ReminderDraft(offer: try offer([3600, 7200, 10_800]))
        draft.toggle("r3")
        draft.toggle("r1")
        draft.setTime(now.addingTimeInterval(9000), of: "r1")
        let picks = draft.picks(now: now)
        XCTAssertEqual(picks?.map(\.id), ["r1", "r3"])
        XCTAssertEqual(picks?[0].at, ReminderDraft.localInput(now.addingTimeInterval(9000)))
        XCTAssertEqual(picks?[1].at, ReminderDraft.localInput(now.addingTimeInterval(10_800)))
    }

    func testChangingTheMomentOfAnOptionChoosesIt() throws {
        var draft = ReminderDraft(offer: try offer([3600]))
        draft.setTime(now.addingTimeInterval(5000), of: "r1")
        XCTAssertTrue(draft.isTicked("r1"))
        draft.toggle("r1")
        XCTAssertFalse(draft.isTicked("r1"))
    }

    func testATickedOptionInThePastCannotBeConfirmedButAnUntickedOneDoesNotMatter() throws {
        var draft = ReminderDraft(offer: try offer([-60, 3600]))
        draft.toggle("r2")
        XCTAssertTrue(draft.canConfirm(now: now))
        draft.toggle("r1")
        XCTAssertFalse(draft.allUpcoming(now: now))
        XCTAssertNil(draft.picks(now: now))
    }

    func testTheLocalMomentIsWrittenToTheMinuteInTheGivenCalendarsZone() {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "Europe/Paris")!
        let date = calendar.date(from: DateComponents(year: 2026, month: 11, day: 20, hour: 9, minute: 5, second: 42))!
        XCTAssertEqual(ReminderDraft.localInput(date, calendar: calendar), "2026-11-20T09:05")
    }

    func testAnOfferIsOnlyProposedWhileItHasOptionsAndWaits() throws {
        XCTAssertTrue(try offer([3600]).isProposed)
        let decided = try SnapshotDecoder.make().decode(ReminderOffer.self, from: Data(#"{ "id": "o", "status": "accepted", "items": [], "chosen": ["r1"] }"#.utf8))
        XCTAssertFalse(decided.isProposed)
    }
}
