import XCTest
@testable import PoupleCore

final class ServerSentEventParserTests: XCTestCase {
    func testAnEventIsCompletedByABlankLine() {
        var parser = ServerSentEventParser()
        XCTAssertNil(parser.feed(line: "event: snapshot"))
        XCTAssertNil(parser.feed(line: "data: {\"a\":1}"))
        XCTAssertEqual(parser.feed(line: ""), ServerSentEvent(name: "snapshot", data: "{\"a\":1}"))
    }

    func testCommentsHeartbeatsAndEmptyEventsAreIgnored() {
        var parser = ServerSentEventParser()
        XCTAssertNil(parser.feed(line: ": ping"))
        XCTAssertNil(parser.feed(line: ""))
    }

    func testSeveralDataLinesAreJoinedAndTheNameResets() {
        var parser = ServerSentEventParser()
        _ = parser.feed(line: "event: snapshot")
        _ = parser.feed(line: "data: one")
        _ = parser.feed(line: "data: two")
        XCTAssertEqual(parser.feed(line: ""), ServerSentEvent(name: "snapshot", data: "one\ntwo"))
        _ = parser.feed(line: "data: next")
        XCTAssertEqual(parser.feed(line: "")?.name, "message")
    }
}

final class PoupleAPITests: XCTestCase {
    override func tearDown() {
        StubProtocol.handler = nil
        super.tearDown()
    }

    private func api() -> PoupleAPI {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [StubProtocol.self]
        return PoupleAPI(connection: CoreConnection(port: 4000, token: "secret-token"), session: URLSession(configuration: configuration))
    }

    func testAnswersAreSentAsTheServerExpectsThem() async throws {
        var seen: (String?, String?, [String: Any])?
        StubProtocol.handler = { request in
            let body = (try? JSONSerialization.jsonObject(with: request.bodyData)) as? [String: Any] ?? [:]
            seen = (request.httpMethod, request.url?.path, body)
            return (202, Data())
        }

        try await api().answer(requestId: "r1", answers: [QuestionAnswer(questionId: "q1", optionId: "A"), QuestionAnswer(questionId: "q2", text: "Lyon")])

        XCTAssertEqual(seen?.0, "POST")
        XCTAssertEqual(seen?.1, "/api/assistant/requests/r1/answers")
        let answers = seen?.2["answers"] as? [[String: String]]
        XCTAssertEqual(answers, [["questionId": "q1", "optionId": "A"], ["questionId": "q2", "text": "Lyon"]])
    }

    func testSeveralPickedOptionsAndATypedAnswerAreSentTogether() async throws {
        var body: [String: Any] = [:]
        StubProtocol.handler = { request in
            body = (try? JSONSerialization.jsonObject(with: request.bodyData)) as? [String: Any] ?? [:]
            return (202, Data())
        }
        try await api().answer(requestId: "r1", answers: [QuestionAnswer(questionId: "q1", optionIds: ["A", "C"], text: "and tell Florian")])
        let answer = (body["answers"] as? [[String: Any]])?.first
        XCTAssertEqual(answer?["optionIds"] as? [String], ["A", "C"])
        XCTAssertEqual(answer?["text"] as? String, "and tell Florian")
        XCTAssertNil(answer?["optionId"])
    }

    func testClosingABubbleTellsTheCoreWhichStateWasClosed() async throws {
        var seen: (String?, String?, [String: Any])?
        StubProtocol.handler = { request in
            seen = (request.httpMethod, request.url?.path, (try? JSONSerialization.jsonObject(with: request.bodyData)) as? [String: Any] ?? [:])
            return (204, Data())
        }
        try await api().close(requestId: "r1", seen: Date(timeIntervalSince1970: 1_794_571_200.25))
        XCTAssertEqual(seen?.0, "POST")
        XCTAssertEqual(seen?.1, "/api/assistant/requests/r1/close")
        XCTAssertEqual(seen?.2["updatedAt"] as? String, "2026-11-13T12:00:00.250Z")
    }

    func testTheProposalDecisionAndEmptyAnswersWork() async throws {
        var body: [String: Any] = [:]
        StubProtocol.handler = { request in
            body = (try? JSONSerialization.jsonObject(with: request.bodyData)) as? [String: Any] ?? [:]
            return (204, Data())
        }
        try await api().decideProposal(requestId: "r1", approve: true)
        XCTAssertEqual(body["decision"] as? String, "approve")
        try await api().ignore(requestId: "r1")
    }

    func testAFreeMessageIsSentToTheRequestsConversation() async throws {
        var seen: (String?, [String: Any])?
        StubProtocol.handler = { request in
            seen = (request.url?.path, (try? JSONSerialization.jsonObject(with: request.bodyData)) as? [String: Any] ?? [:])
            return (202, Data())
        }
        try await api().message(requestId: "r1", text: "and the Android one?")
        XCTAssertEqual(seen?.0, "/api/assistant/requests/r1/messages")
        XCTAssertEqual(seen?.1["text"] as? String, "and the Android one?")
    }

    func testEveryCallCarriesTheBearerTokenInAHeaderNeverInTheUrl() async throws {
        var seen: [URLRequest] = []
        StubProtocol.handler = { request in
            seen.append(request)
            return (200, Data(#"{"setupComplete":true,"steps":[{"id":"slack","name":"Slack","required":true,"connected":true}]}"#.utf8))
        }
        let state = try await api().setupState()
        try await api().stop(requestId: "r1")
        try await api().delete(requestId: "r1")

        XCTAssertTrue(state.setupComplete)
        XCTAssertEqual(state.steps.first?.id, "slack")
        XCTAssertEqual(seen.map(\.httpMethod), ["GET", "POST", "DELETE"])
        XCTAssertEqual(seen.map { $0.url?.path }, ["/api/state", "/api/assistant/requests/r1/stop", "/api/assistant/requests/r1"])
        for request in seen {
            XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer secret-token")
            XCTAssertEqual(request.url?.host, "127.0.0.1")
            XCTAssertFalse(request.url?.absoluteString.contains("secret-token") ?? true)
        }
    }

    func testTheLanguageIsSwitchedThroughTheSettings() async throws {
        var seen: (String?, String?, [String: Any])?
        StubProtocol.handler = { request in
            seen = (request.httpMethod, request.url?.path, (try? JSONSerialization.jsonObject(with: request.bodyData)) as? [String: Any] ?? [:])
            return (200, Data("{}".utf8))
        }
        try await api().setUILanguage(.en)
        XCTAssertEqual(seen?.0, "PUT")
        XCTAssertEqual(seen?.1, "/api/settings")
        XCTAssertEqual(seen?.2["uiLanguage"] as? String, "en")
    }

    func testTheStreamRequestIsAuthorizedToo() {
        let request = api().streamRequest()
        XCTAssertEqual(request.url?.absoluteString, "http://127.0.0.1:4000/api/assistant/stream")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer secret-token")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Accept"), "text/event-stream")
    }

    func testAServerErrorKeepsItsMessage() async {
        StubProtocol.handler = { _ in (409, Data(#"{"error":"The request is ready, not awaiting-user"}"#.utf8)) }
        do {
            try await api().ignore(requestId: "r1")
            XCTFail("Expected an error")
        } catch {
            XCTAssertEqual(error as? PoupleAPIError, .http(status: 409, message: "The request is ready, not awaiting-user"))
        }
    }

    /// What the Go core really sends. The bubbles were silently empty once because the core did not name its events.
    private func firstSnapshot(from body: String) async throws -> AssistantSnapshot? {
        StubProtocol.handler = { _ in (200, Data(body.utf8)) }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [StubProtocol.self]
        let stream = SnapshotStream(request: api().streamRequest(), session: URLSession(configuration: configuration))
        for try await snapshot in stream.snapshots() { return snapshot }
        return nil
    }

    func testTheStreamYieldsTheSnapshotsTheCoreSends() async throws {
        let compact = ModelsTests.snapshotJSON.replacingOccurrences(of: "\n", with: " ")
        let named = try await firstSnapshot(from: ": keep-alive\n\nevent: snapshot\ndata: \(compact)\n\n")
        XCTAssertEqual(named?.requests.count, 2)
        let unnamed = try await firstSnapshot(from: "data: \(compact)\n\n")
        XCTAssertEqual(unnamed?.requests.count, 2, "an unnamed event is the same payload and must not be dropped")
        let other = try await firstSnapshot(from: "event: something-else\ndata: \(compact)\n\n")
        XCTAssertNil(other, "events of another name are not snapshots")
    }

    func testSnapshotDecodesTheResponse() async throws {
        StubProtocol.handler = { _ in (200, Data(ModelsTests.snapshotJSON.utf8)) }
        let snapshot = try await api().snapshot()
        XCTAssertEqual(snapshot.requests.count, 2)
    }
}

/// Answers every request of the session with what the test set up, without any network.
final class StubProtocol: URLProtocol {
    nonisolated(unsafe) static var handler: ((URLRequest) -> (Int, Data))?

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let (status, data) = Self.handler?(request) ?? (500, Data())
        let response = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: nil)!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: data)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}

private extension URLRequest {
    /// URLProtocol receives the body as a stream, not as `httpBody`.
    var bodyData: Data {
        if let httpBody { return httpBody }
        guard let stream = httpBodyStream else { return Data() }
        stream.open()
        defer { stream.close() }
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 1024)
        while stream.hasBytesAvailable {
            let count = stream.read(&buffer, maxLength: buffer.count)
            if count <= 0 { break }
            data.append(buffer, count: count)
        }
        return data
    }
}
