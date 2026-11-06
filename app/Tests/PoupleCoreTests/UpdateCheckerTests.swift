import XCTest
@testable import PoupleCore

final class UpdateCheckerTests: XCTestCase {
    func testANewerSourceReleaseUsesTheOfficialInstallInstructions() async throws {
        let checker = checker(tag: "v1.10.0")
        let result = try await checker.check(installedVersion: "1.9.12")
        XCTAssertEqual(
            result,
            .available(
                version: "1.10.0",
                instructionsURL: URL(string: "https://arthur-monnet.github.io/pouple-app/#install")!
            )
        )
    }

    func testAnEqualOrOlderReleaseDoesNotOfferADowngrade() async throws {
        let checker = checker(tag: "v1.9.0")
        for installed in ["1.9.0", "1.10.0", "2.0.0"] {
            let result = try await checker.check(installedVersion: installed)
            XCTAssertEqual(result, .upToDate)
        }
    }

    func testAnUnreadableOrPrivateReleaseHasADistinctFailure() async {
        await assertFailure(.releaseUnavailable, checking: checker(status: 404))
        await assertFailure(.releaseUnavailable, checking: checker(draft: true))
        await assertFailure(.releaseUnavailable, checking: checker(prerelease: true))
    }

    func testMalformedVersionsCannotOfferAnUpdate() async {
        for tag in ["v2.0.0-rc.1", "v2.0.0/../../redirect", "v02.0.0", "v2..0", "2.0.0"] {
            await assertFailure(.unexpectedResponse, checking: checker(tag: tag))
        }
        let checker = UpdateChecker { _ in
            XCTFail("A build without a valid version should not query GitHub")
            throw UpdateChecker.Failure.unexpectedResponse
        }
        await assertFailure(.invalidInstalledVersion, checking: checker, installed: "")
    }

    func testServerErrorsAreNotReportedAsUpToDate() async {
        await assertFailure(.unexpectedResponse, checking: checker(status: 403))
        await assertFailure(.unexpectedResponse, checking: checker(status: 503))
    }

    private func checker(
        tag: String = "v2.0.0", status: Int = 200, draft: Bool = false, prerelease: Bool = false
    ) -> UpdateChecker {
        UpdateChecker { request in
            let url = try XCTUnwrap(request.url)
            XCTAssertEqual(url.absoluteString, "https://api.github.com/repos/Arthur-MONNET/pouple-app/releases/latest")
            XCTAssertEqual(request.value(forHTTPHeaderField: "Accept"), "application/vnd.github+json")
            XCTAssertNil(request.value(forHTTPHeaderField: "Authorization"))
            let data = try JSONSerialization.data(withJSONObject: [
                "tag_name": tag, "draft": draft, "prerelease": prerelease,
                "html_url": "https://example.com/untrusted-release",
            ])
            let response = try XCTUnwrap(HTTPURLResponse(url: url, statusCode: status, httpVersion: nil, headerFields: nil))
            return (data, response)
        }
    }

    private func assertFailure(
        _ expected: UpdateChecker.Failure, checking checker: UpdateChecker, installed: String = "1.0.0",
        file: StaticString = #filePath, line: UInt = #line
    ) async {
        do {
            _ = try await checker.check(installedVersion: installed)
            XCTFail("Expected \(expected)", file: file, line: line)
        } catch {
            XCTAssertEqual(error as? UpdateChecker.Failure, expected, file: file, line: line)
        }
    }
}
