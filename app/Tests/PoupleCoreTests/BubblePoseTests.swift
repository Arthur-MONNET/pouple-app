import XCTest
@testable import PoupleCore

final class BubblePoseTests: XCTestCase {
    func testPoseFollowsWhatTheBubbleAsks() {
        XCTAssertEqual(BubblePose(kind: .decision), .waiting)
        XCTAssertEqual(BubblePose(kind: .working), .working)
        XCTAssertEqual(BubblePose(kind: .result), .done)
        XCTAssertEqual(BubblePose(kind: .done), .done)
        XCTAssertEqual(BubblePose(kind: .failed), .failed)
    }

    func testEveryPoseHasItsOwnDrawing() {
        let resources = BubblePose.allCases.map(\.resource)
        XCTAssertEqual(Set(resources).count, resources.count)
        XCTAssertEqual(Set(resources), ["investigate", "multitask", "done", "oops"])
    }
}
