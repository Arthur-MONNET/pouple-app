import XCTest
@testable import PoupleCore

/// A core that does what the test scripts: prints lines, exits on demand, ignores signals.
final class FakeProcess: CoreProcess, @unchecked Sendable {
    let lines: AsyncStream<String>
    private let output: AsyncStream<String>.Continuation
    private let exit = Latch<Int32>()
    private let record = LockedBox<[String]>([])

    /// Which shutdown steps make this process end.
    var exitsOnClose = true
    var exitsOnTerminate = true

    init() {
        var continuation: AsyncStream<String>.Continuation!
        lines = AsyncStream { continuation = $0 }
        output = continuation
    }

    var events: [String] { record.mutate { $0 } }

    func print(_ line: String) { output.yield(line) }
    func die(status: Int32 = 1) {
        output.finish()
        exit.set(status)
    }

    func waitForExit() async -> Int32? { await exit.wait() }
    func closeInput() {
        record.mutate { $0.append("close") }
        if exitsOnClose { die(status: 0) }
    }
    func terminate() {
        record.mutate { $0.append("terminate") }
        if exitsOnTerminate { die(status: 143) }
    }
    func kill() {
        record.mutate { $0.append("kill") }
        die(status: 137)
    }
}

final class FakeLauncher: CoreLauncher, @unchecked Sendable {
    private let state = LockedBox<(processes: [FakeProcess], tokens: [String], script: [() throws -> FakeProcess])>(([], [], []))

    /// What each successive launch does; the last entry repeats.
    init(_ script: [() throws -> FakeProcess]) {
        state.mutate { $0.script = script }
    }

    var processes: [FakeProcess] { state.mutate { $0.processes } }
    var tokens: [String] { state.mutate { $0.tokens } }

    func launch(token: String) throws -> CoreProcess {
        let step: () throws -> FakeProcess = state.mutate { value in
            value.tokens.append(token)
            let index = min(value.tokens.count - 1, value.script.count - 1)
            return value.script[index]
        }
        let process = try step()
        state.mutate { $0.processes.append(process) }
        return process
    }
}

@MainActor
final class CoreSupervisorTests: XCTestCase {
    private let fast = CoreSupervisor.Timing(
        readyTimeout: .milliseconds(150),
        grace: .milliseconds(60),
        stableAfter: .seconds(60),
        backoff: Backoff(base: .milliseconds(20), cap: .milliseconds(80))
    )

    private func supervisor(_ launcher: FakeLauncher) -> CoreSupervisor {
        let counter = LockedBox(0)
        return CoreSupervisor(launcher: launcher, timing: fast, makeToken: { "token-\(counter.mutate { $0 += 1; return $0 })" })
    }

    private func readyProcess(port: Int = 4242) -> () throws -> FakeProcess {
        {
            let process = FakeProcess()
            process.print(#"{"ready":true,"port":\#(port)}"#)
            return process
        }
    }

    func testItReportsStartingThenReadyWithThePortAndConnection() async throws {
        let launcher = FakeLauncher([readyProcess(port: 5555)])
        let supervisor = supervisor(launcher)
        XCTAssertEqual(supervisor.state, .starting)
        supervisor.start()
        try await waitUntil { supervisor.state == .ready(port: 5555) }
        XCTAssertEqual(supervisor.connection, CoreConnection(port: 5555, token: "token-1"))
        XCTAssertEqual(supervisor.connection?.baseURL.absoluteString, "http://127.0.0.1:5555")
        await supervisor.shutdown()
    }

    func testACrashRestartsTheCoreWithAFreshToken() async throws {
        let launcher = FakeLauncher([readyProcess(port: 1111), readyProcess(port: 2222)])
        let supervisor = supervisor(launcher)
        supervisor.start()
        try await waitUntil { supervisor.state == .ready(port: 1111) }

        launcher.processes[0].die(status: 2)
        try await waitUntil { supervisor.state == .ready(port: 2222) }

        XCTAssertEqual(launcher.tokens, ["token-1", "token-2"])
        XCTAssertEqual(supervisor.connection?.token, "token-2")
        await supervisor.shutdown()
    }

    func testACrashIsVisibleAsFailedBeforeTheRestart() async throws {
        let launcher = FakeLauncher([readyProcess()])
        var timing = fast
        timing.backoff = Backoff(base: .milliseconds(300), cap: .milliseconds(300))
        let supervisor = CoreSupervisor(launcher: launcher, timing: timing)
        supervisor.start()
        try await waitUntil { supervisor.state == .ready(port: 4242) }
        launcher.processes[0].die(status: 9)
        try await waitUntil { if case .failed = supervisor.state { return true } else { return false } }
        XCTAssertNil(supervisor.connection)
        XCTAssertEqual(supervisor.state, .failed(.stopped(status: 9)))
        XCTAssertTrue(CoreFailure.stopped(status: 9).message(language: .en).contains("exit status 9"))
        await supervisor.shutdown()
    }

    func testAMalformedFirstLineFailsStopsTheProcessAndTriesAgain() async throws {
        let bad: () throws -> FakeProcess = {
            let process = FakeProcess()
            process.print("panic: something")
            return process
        }
        let launcher = FakeLauncher([bad, readyProcess(port: 3333)])
        let supervisor = supervisor(launcher)
        supervisor.start()
        try await waitUntil { supervisor.state == .ready(port: 3333) }
        XCTAssertTrue(launcher.processes[0].events.contains("close"))
        await supervisor.shutdown()
    }

    func testSilenceEndsInATimeoutThenARetry() async throws {
        let silent: () throws -> FakeProcess = { FakeProcess() }
        let launcher = FakeLauncher([silent, readyProcess(port: 3334)])
        let supervisor = supervisor(launcher)
        supervisor.start()
        try await waitUntil { if case .failed = supervisor.state { return true } else { return false } }
        XCTAssertEqual(supervisor.state, .failed(.timeout(seconds: 0)))
        try await waitUntil { supervisor.state == .ready(port: 3334) }
        await supervisor.shutdown()
    }

    func testAnOutputThatClosesBeforeTheReadyLineIsAFailure() async throws {
        let dies: () throws -> FakeProcess = {
            let process = FakeProcess()
            process.die(status: 1)
            return process
        }
        let launcher = FakeLauncher([dies, readyProcess(port: 3335)])
        let supervisor = supervisor(launcher)
        supervisor.start()
        try await waitUntil { supervisor.state == .ready(port: 3335) }
        await supervisor.shutdown()
    }

    func testALaunchErrorIsReportedAndRetriedWithGrowingDelays() async throws {
        struct Missing: LocalizedError { var errorDescription: String? { "The core is missing" } }
        let launcher = FakeLauncher([{ throw Missing() }])
        let supervisor = supervisor(launcher)
        supervisor.start()
        try await waitUntil { launcher.tokens.count >= 3 }
        XCTAssertEqual(supervisor.state, .failed(.other("The core is missing")))
        await supervisor.shutdown()
    }

    func testRetryNowSkipsTheBackoff() async throws {
        let launcher = FakeLauncher([{ throw CoreLaunchError.binaryMissing("/x") }, readyProcess(port: 3336)])
        var timing = fast
        timing.backoff = Backoff(base: .seconds(30), cap: .seconds(30))
        let supervisor = CoreSupervisor(launcher: launcher, timing: timing)
        supervisor.start()
        try await waitUntil { supervisor.state == .failed(.binaryMissing(path: "/x")) }
        supervisor.retryNow()
        try await waitUntil { supervisor.state == .ready(port: 3336) }
        await supervisor.shutdown()
    }

    func testShutdownClosesTheInputFirstAndNothingMore() async throws {
        let launcher = FakeLauncher([readyProcess()])
        let supervisor = supervisor(launcher)
        supervisor.start()
        try await waitUntil { supervisor.state == .ready(port: 4242) }
        await supervisor.shutdown()
        XCTAssertEqual(launcher.processes[0].events, ["close"])
        XCTAssertNil(supervisor.connection)
        // Nothing restarts after a shutdown.
        try await Task.sleep(for: .milliseconds(100))
        XCTAssertEqual(launcher.processes.count, 1)
    }

    func testShutdownEscalatesToTerminateThenKill() async throws {
        let stubborn: () throws -> FakeProcess = {
            let process = FakeProcess()
            process.exitsOnClose = false
            process.exitsOnTerminate = false
            process.print(#"{"ready":true,"port":4242}"#)
            return process
        }
        let launcher = FakeLauncher([stubborn])
        let supervisor = supervisor(launcher)
        supervisor.start()
        try await waitUntil { supervisor.state == .ready(port: 4242) }
        await supervisor.shutdown()
        XCTAssertEqual(launcher.processes[0].events, ["close", "terminate", "kill"])
    }

    func testShutdownDuringStartupLeavesNoProcessBehind() async throws {
        let silent: () throws -> FakeProcess = { FakeProcess() }
        let launcher = FakeLauncher([silent])
        let supervisor = supervisor(launcher)
        supervisor.start()
        try await waitUntil { launcher.processes.count == 1 }
        await supervisor.shutdown()
        XCTAssertTrue(launcher.processes[0].events.contains("close"))
        XCTAssertEqual(launcher.processes.count, 1)
    }
}
