import XCTest
@testable import PoupleCore

final class ModelsTests: XCTestCase {
    /// The shape GET /api/assistant returns (see shared/assistant.ts), including values this build does not know.
    static let snapshotJSON = """
    {
      "status": { "active": true, "problem": null, "lastPollAt": "2026-11-05T14:53:19.709Z", "notices": ["Group chats: missing scope"] },
      "requests": [
        {
          "id": "a",
          "source": { "origin": "thread-reply", "channelId": "C1", "channelName": "front", "senderId": "U9", "senderName": "Morgan", "ts": "1.1", "permalink": "https://x/p1" },
          "text": "any update?", "context": "",
          "receivedAt": "2026-11-05T14:00:00.000Z", "updatedAt": "2026-11-05T14:02:00Z",
          "status": "awaiting-user", "summary": "Found it", "draftReply": null,
          "questions": [{ "id": "q1", "text": "Which flow?", "options": [{ "id": "A", "label": "Email", "description": null }] }],
          "proposal": { "title": "Fix login", "description": "Trim", "projectId": "vision" },
          "entries": [{ "id": "e1", "at": "2026-11-05T14:00:00Z", "role": "assistant", "kind": "step", "text": "$ ls" }],
          "error": null
        },
        {
          "id": "b",
          "source": { "origin": "carrier-pigeon", "channelId": "D1", "channelName": null, "senderId": "U1", "senderName": "Arthur", "ts": "2.2", "permalink": null },
          "text": "hi", "context": "", "receivedAt": "2026-11-05T14:00:00Z", "updatedAt": "2026-11-05T14:00:00Z",
          "status": "teleporting", "summary": null, "draftReply": null, "questions": [], "proposal": null, "entries": [], "error": null
        }
      ]
    }
    """

    func testDecodesTheServerSnapshot() throws {
        let snapshot = try SnapshotDecoder.make().decode(AssistantSnapshot.self, from: Data(Self.snapshotJSON.utf8))

        XCTAssertTrue(snapshot.status.active)
        XCTAssertEqual(snapshot.status.notices, ["Group chats: missing scope"])
        let first = try XCTUnwrap(snapshot.requests.first)
        XCTAssertEqual(first.source.origin, .threadReply)
        XCTAssertEqual(first.status, .awaitingUser)
        XCTAssertEqual(first.questions.first?.options.first?.label, "Email")
        XCTAssertEqual(first.proposal?.projectId, "vision")
        XCTAssertEqual(first.entries.first?.kind, .step)
    }

    func testTheEngagementTheClosingAndSeveralOptionsAreReadAndDefaultToOff() throws {
        let snapshot = try SnapshotDecoder.make().decode(AssistantSnapshot.self, from: Data(Self.snapshotJSON.utf8))
        let first = try XCTUnwrap(snapshot.requests.first)
        XCTAssertFalse(first.isEngaged)
        XCTAssertFalse(first.isClosed)
        XCTAssertEqual(first.questions.first?.allowsSeveral, false)

        let json = Self.snapshotJSON
            .replacingOccurrences(of: #""proposal": { "title": "Fix login""#, with: #""engaged": true, "closed": true, "proposal": { "title": "Fix login""#)
            .replacingOccurrences(of: #""text": "Which flow?","#, with: #""text": "Which flow?", "multiple": true,"#)
        let flagged = try XCTUnwrap(SnapshotDecoder.make().decode(AssistantSnapshot.self, from: Data(json.utf8)).requests.first)
        XCTAssertTrue(flagged.isEngaged)
        XCTAssertTrue(flagged.isClosed)
        XCTAssertEqual(flagged.questions.first?.allowsSeveral, true)
    }

    func testAPipelineRequestIsTitledByItsHeadline() throws {
        let json = """
        {
          "id": "p", "source": { "origin": "pipeline", "channelId": "gitlab-pipelines", "channelName": "GitLab", "senderId": "gitlab", "senderName": "GitLab", "ts": "3.0", "permalink": null },
          "text": "❌ dating/vision · feat\\nPipeline #9 failed", "context": "", "receivedAt": "2026-11-05T14:00:00Z", "updatedAt": "2026-11-05T14:00:00Z",
          "status": "ready", "summary": null, "draftReply": null, "questions": [], "proposal": null, "entries": [], "error": null
        }
        """
        let request = try SnapshotDecoder.make().decode(AssistantRequest.self, from: Data(json.utf8))
        XCTAssertEqual(request.source.origin, .pipeline)
        XCTAssertEqual(request.title, "❌ dating/vision · feat")
    }

    func testAReminderThatCameDueIsTitledByItsAlertAndCarriesTheSuggestedReminders() throws {
        let json = """
        {
          "id": "reminder-1", "source": { "origin": "reminder", "channelId": "reminders", "channelName": null, "senderId": "pouple", "senderName": "Pouple", "ts": "", "permalink": null, "relatedRequestId": "a" },
          "text": "Confirm your leave to Jocelyn\\nFrom the Slack thread", "context": "", "receivedAt": "2026-11-05T14:00:00Z", "updatedAt": "2026-11-05T14:00:00Z",
          "status": "ready", "summary": "From the Slack thread", "draftReply": null, "questions": [], "proposal": null, "entries": [], "error": null, "engaged": true,
          "reminders": { "id": "o1", "status": "proposed", "items": [{ "id": "r1", "label": "Call back", "at": "2026-11-20T09:00:00+01:00" }] }
        }
        """
        let request = try SnapshotDecoder.make().decode(AssistantRequest.self, from: Data(json.utf8))
        XCTAssertEqual(request.reminders?.items.first?.at, Date(timeIntervalSince1970: 1_795_161_600))
        XCTAssertEqual(request.reminders?.isProposed, true)
        XCTAssertEqual(request.source.origin, .reminder)
        XCTAssertEqual(request.title, "Confirm your leave to Jocelyn")
        XCTAssertTrue(request.isEngaged)
    }

    func testOnlyAPlanOfOneActionCanBeApprovedFromTheBubble() throws {
        func plan(id: String?, status: String = "proposed", actions: Int) throws -> AssistantPlan {
            let action = #"{"kind":"code.edit","scope":{"service":"code","id":"login-app"},"summary":"Keep the page","branch":"fix/login"}"#
            let idField = id.map { #""id":"\#($0)","# } ?? ""
            let json = #"{\#(idField)"goal":"Fix the login redirect","brief":"b","status":"\#(status)","actions":[\#(Array(repeating: action, count: actions).joined(separator: ","))]}"#
            return try JSONDecoder().decode(AssistantPlan.self, from: Data(json.utf8))
        }
        XCTAssertTrue(try plan(id: "p1", actions: 1).canApproveAtOnce)
        // Several courses to tick: approving them all blindly could run two that exclude each other.
        XCTAssertFalse(try plan(id: "p1", actions: 2).canApproveAtOnce)
        // An approval names the plan that was read; and a plan already decided waits for nothing.
        XCTAssertFalse(try plan(id: nil, actions: 1).canApproveAtOnce)
        XCTAssertFalse(try plan(id: "p1", status: "approved", actions: 1).canApproveAtOnce)
    }

    func testDatesWithAndWithoutMillisecondsBothDecode() throws {
        let snapshot = try SnapshotDecoder.make().decode(AssistantSnapshot.self, from: Data(Self.snapshotJSON.utf8))
        let first = try XCTUnwrap(snapshot.requests.first)
        XCTAssertEqual(first.updatedAt.timeIntervalSince(first.receivedAt), 120, accuracy: 0.001)
    }

    func testUnknownValuesFromANewerServerDoNotBreakDecoding() throws {
        let snapshot = try SnapshotDecoder.make().decode(AssistantSnapshot.self, from: Data(Self.snapshotJSON.utf8))
        XCTAssertEqual(snapshot.requests.last?.source.origin, .unknown)
        XCTAssertEqual(snapshot.requests.last?.status, .unknown)
    }

    func testTitlesNameTheSenderAndWhereTheMessageCameFrom() throws {
        let snapshot = try SnapshotDecoder.make().decode(AssistantSnapshot.self, from: Data(Self.snapshotJSON.utf8))
        XCTAssertEqual(snapshot.requests.first?.title(language: .en), "Morgan · thread in #front")
        XCTAssertEqual(snapshot.requests.first?.title(language: .fr), "Morgan · fil dans #front")
    }

    func testAnActionStartedFromPoupleIsTitledByItsLabel() throws {
        let json = Self.snapshotJSON.replacingOccurrences(
            of: #""origin": "carrier-pigeon", "channelId": "D1", "channelName": null, "senderId": "U1", "senderName": "Arthur", "ts": "2.2", "permalink": null"#,
            with: #""origin": "pouple-action", "channelId": "action:run-app:vision", "channelName": null, "senderId": "pouple", "senderName": "Pouple", "ts": "2.2", "permalink": null, "label": "Run vision on iOS""#
        )
        let snapshot = try SnapshotDecoder.make().decode(AssistantSnapshot.self, from: Data(json.utf8))
        XCTAssertEqual(snapshot.requests.last?.source.origin, .poupleAction)
        XCTAssertEqual(snapshot.requests.last?.title, "Run vision on iOS")
    }
}
