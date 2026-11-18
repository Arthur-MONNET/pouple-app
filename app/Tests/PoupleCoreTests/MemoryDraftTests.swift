import XCTest
@testable import PoupleCore

final class MemoryDraftTests: XCTestCase {
    private func offer(ticked: [Bool]) throws -> MemoryOffer {
        let items = ticked.enumerated().map { index, isTicked in
            #"{ "id": "m\#(index + 1)", "noteTitle": "Note \#(index + 1)", "fact": "Fact \#(index + 1)", "ticked": \#(isTicked) }"#
        }
        let json = #"{ "id": "o1", "status": "proposed", "items": [\#(items.joined(separator: ","))] }"#
        return try SnapshotDecoder.make().decode(MemoryOffer.self, from: Data(json.utf8))
    }

    func testFactsStartTickedAsProposedAndPrivate() throws {
        let draft = MemoryDraft(offer: try offer(ticked: [true, false, true]))
        XCTAssertEqual(draft.chosen.map(\.id), ["m1", "m3"])
        XCTAssertEqual(draft.picks(), [MemoryPick(id: "m1", shared: false), MemoryPick(id: "m3", shared: false)])
    }

    func testOnlyASharedFactIsSentAsShared() throws {
        var draft = MemoryDraft(offer: try offer(ticked: [true, true]))
        draft.toggleShared("m2")
        XCTAssertEqual(draft.picks(), [MemoryPick(id: "m1", shared: false), MemoryPick(id: "m2", shared: true)])
        draft.toggleShared("m2")
        XCTAssertEqual(draft.picks()?.last?.shared, false)
    }

    func testNothingTickedCannotBeConfirmed() throws {
        var draft = MemoryDraft(offer: try offer(ticked: [true]))
        draft.toggle("m1")
        XCTAssertFalse(draft.canConfirm)
        XCTAssertNil(draft.picks())
    }

    func testAnUntickedFactIsNeverSentEvenIfMadeShared() throws {
        var draft = MemoryDraft(offer: try offer(ticked: [true, true]))
        draft.toggleShared("m1")
        draft.toggle("m1")
        XCTAssertEqual(draft.picks()?.map(\.id), ["m2"])
    }

    func testAnOfferIsOnlyProposedWhileItHasFactsAndWaits() throws {
        XCTAssertTrue(try offer(ticked: [true]).isProposed)
        let decided = try SnapshotDecoder.make().decode(MemoryOffer.self, from: Data(#"{ "id": "o", "status": "accepted", "items": [], "chosen": ["m1"] }"#.utf8))
        XCTAssertFalse(decided.isProposed)
    }

    func testTheOfferDecodesItsOptionalFields() throws {
        let json = #"{ "id": "o", "status": "proposed", "items": [{ "id": "m1", "note": "people/jocelyn.md", "noteTitle": "Jocelyn", "fact": "Likes tea", "replaces": "Likes coffee", "ticked": true }] }"#
        let offer = try SnapshotDecoder.make().decode(MemoryOffer.self, from: Data(json.utf8))
        XCTAssertEqual(offer.items.first?.note, "people/jocelyn.md")
        XCTAssertEqual(offer.items.first?.replaces, "Likes coffee")
    }
}
