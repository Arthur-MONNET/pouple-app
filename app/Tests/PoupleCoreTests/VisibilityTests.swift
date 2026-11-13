import XCTest
@testable import PoupleCore

final class VisibilityTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func request(
        _ id: String,
        _ status: RequestStatus,
        received: TimeInterval = 0,
        updatedAgo: TimeInterval = 0,
        engaged: Bool? = nil,
        closed: Bool? = nil
    ) -> AssistantRequest {
        let json = """
        {"id":"\(id)","source":{"origin":"mention","channelId":"C1","channelName":"front","senderId":"U","senderName":"Morgan","ts":"1","permalink":null},
         "text":"hi","context":"","receivedAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z","status":"\(status.rawValue)",
         "summary":null,"draftReply":null,"questions":[],"proposal":null,"entries":[],"error":null}
        """
        let decoded = try! SnapshotDecoder.make().decode(AssistantRequestEnvelope.self, from: Data(json.utf8)).value
        return AssistantRequest(
            id: id, source: decoded.source, text: decoded.text,
            receivedAt: now.addingTimeInterval(received), updatedAt: now.addingTimeInterval(-updatedAgo),
            status: status, summary: nil, draftReply: nil, questions: [], proposal: nil, entries: [], error: nil,
            engaged: engaged, closed: closed
        )
    }

    private func kinds(_ layout: BubbleLayout) -> [String: BubbleKind] {
        Dictionary(uniqueKeysWithValues: layout.items.map { ($0.id, $0.kind) })
    }

    func testDecisionsAndFailuresStayUntilHandledAndSilentWorkHasNoBubble() {
        let layout = BubbleVisibility.layout(
            requests: [
                request("q", .queued), request("a", .analyzing), request("p", .applying),
                request("d", .awaitingUser, updatedAgo: 3600), request("f", .failed, updatedAgo: 3600),
            ],
            now: now, dismissed: [:]
        )
        XCTAssertEqual(kinds(layout), ["d": .decision, "f": .failed])
    }

    func testOnceTheUserIsInTheConversationTheWorkShowsInTheBubble() {
        let layout = BubbleVisibility.layout(
            requests: [request("a", .analyzing, engaged: true), request("p", .applying, engaged: true), request("q", .queued, engaged: true)],
            now: now, dismissed: [:]
        )
        XCTAssertEqual(kinds(layout), ["a": .working, "p": .working, "q": .working])
    }

    func testOnlyQueuedAnalyzingAndApplyingCountAsWorkInProgress() {
        let working: Set<RequestStatus> = [.queued, .analyzing, .applying]
        for status in [RequestStatus.queued, .analyzing, .awaitingUser, .ready, .applying, .done, .ignored, .failed, .unknown] {
            XCTAssertEqual(status.isInProgress, working.contains(status), "\(status)")
        }
    }

    func testAnAnswerStaysUntilTheUserSaysItIsDone() {
        let old = request("old", .ready, updatedAgo: 86_400, engaged: true)
        XCTAssertEqual(kinds(BubbleVisibility.layout(requests: [old], now: now, dismissed: [:])), ["old": .result])

        let closed = request("old", .ready, updatedAgo: 86_400, engaged: true, closed: true)
        XCTAssertTrue(BubbleVisibility.layout(requests: [closed], now: now, dismissed: [:]).items.isEmpty)
    }

    func testAnswersFromBeforeTheCoreTrackedThemNeverComeBack() {
        // A core that does not send "engaged" yet: its finished answers must not all pop up at once.
        XCTAssertTrue(BubbleVisibility.layout(requests: [request("legacy", .ready, updatedAgo: 5)], now: now, dismissed: [:]).items.isEmpty)
    }

    func testAChangeMadeIsConfirmedBrieflyUnlessThePointerIsOnIt() {
        let fresh = request("fresh", .done, updatedAgo: 3, engaged: true)
        let old = request("old", .done, updatedAgo: BubbleVisibility.confirmationLifetime + 1, engaged: true)
        XCTAssertEqual(kinds(BubbleVisibility.layout(requests: [fresh, old], now: now, dismissed: [:])), ["fresh": .done])
        XCTAssertEqual(BubbleVisibility.layout(requests: [old], now: now, dismissed: [:], lingering: ["old"]).items.count, 1)
    }

    func testAClosedBubbleStaysClosedWhateverItsState() {
        let requests = [
            request("d", .awaitingUser, closed: true), request("w", .analyzing, engaged: true, closed: true),
            request("f", .failed, closed: true), request("r", .ready, engaged: true, closed: true),
        ]
        XCTAssertTrue(BubbleVisibility.layout(requests: requests, now: now, dismissed: [:]).items.isEmpty)
    }

    func testIgnoredAndUnknownRequestsNeverInterrupt() {
        let layout = BubbleVisibility.layout(requests: [request("i", .ignored), request("u", .unknown)], now: now, dismissed: [:])
        XCTAssertTrue(layout.items.isEmpty)
    }

    func testADismissedBubbleComesBackOnlyWhenItsRequestChanges() {
        let pending = request("d", .awaitingUser, updatedAgo: 60)
        let closedAfter = pending.updatedAt.addingTimeInterval(1)
        XCTAssertTrue(BubbleVisibility.layout(requests: [pending], now: now, dismissed: ["d": closedAfter]).items.isEmpty)

        let changedLater = request("d", .awaitingUser, updatedAgo: 0)
        XCTAssertEqual(BubbleVisibility.layout(requests: [changedLater], now: now, dismissed: ["d": closedAfter]).items.count, 1)
    }

    func testTheNewestBubbleIsLastAndOverflowIsCounted() {
        let requests = (0 ..< 8).map { request("r\($0)", .awaitingUser, received: TimeInterval($0)) }
        let layout = BubbleVisibility.layout(requests: requests.shuffled(), now: now, dismissed: [:])
        XCTAssertEqual(layout.items.map(\.id), ["r3", "r4", "r5", "r6", "r7"])
        XCTAssertEqual(layout.overflow, 3)
    }
}

/// Test helper: decodes just enough of a request to reuse its source.
private struct AssistantRequestEnvelope: Decodable {
    struct Value: Decodable { let source: RequestSource; let text: String }
    let value: Value
    init(from decoder: Decoder) throws { value = try Value(from: decoder) }
}
