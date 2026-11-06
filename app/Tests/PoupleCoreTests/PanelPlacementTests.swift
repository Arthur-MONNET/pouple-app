import XCTest
@testable import PoupleCore

final class PanelPlacementTests: XCTestCase {
    /// A 2560x1440 screen whose usable area starts above a 30 pt bottom dock and stops below a 30 pt menu bar.
    private let area = CGRect(x: 0, y: 30, width: 2560, height: 1380)
    private let size = CGSize(width: 420, height: 400)

    func testTheNearestCornerIsTheQuadrantOfThePoint() {
        XCTAssertEqual(PanelPlacement.nearestCorner(to: CGPoint(x: 100, y: 1300), in: area), .topLeft)
        XCTAssertEqual(PanelPlacement.nearestCorner(to: CGPoint(x: 2400, y: 1300), in: area), .topRight)
        XCTAssertEqual(PanelPlacement.nearestCorner(to: CGPoint(x: 100, y: 200), in: area), .bottomLeft)
        XCTAssertEqual(PanelPlacement.nearestCorner(to: CGPoint(x: 2400, y: 200), in: area), .bottomRight)
    }

    func testAScreenOnTheRightIsJudgedByItsOwnCenter() {
        let second = CGRect(x: 2560, y: 0, width: 2560, height: 1440)
        XCTAssertEqual(PanelPlacement.nearestCorner(to: CGPoint(x: 2700, y: 100), in: second), .bottomLeft)
        XCTAssertEqual(PanelPlacement.nearestCorner(to: CGPoint(x: 5000, y: 100), in: second), .bottomRight)
    }

    func testEachCornerHasItsFrameWithTheMargin() {
        let margin: CGFloat = 8
        XCTAssertEqual(PanelPlacement.frame(corner: .bottomLeft, size: size, in: area, margin: margin).origin, CGPoint(x: 8, y: 38))
        XCTAssertEqual(PanelPlacement.frame(corner: .bottomRight, size: size, in: area, margin: margin).origin, CGPoint(x: 2132, y: 38))
        XCTAssertEqual(PanelPlacement.frame(corner: .topLeft, size: size, in: area, margin: margin).origin, CGPoint(x: 8, y: 1002))
        XCTAssertEqual(PanelPlacement.frame(corner: .topRight, size: size, in: area, margin: margin).origin, CGPoint(x: 2132, y: 1002))
    }

    func testTheCornerStaysPutWhenTheHeightChanges() {
        let short = PanelPlacement.frame(corner: .bottomRight, size: CGSize(width: 420, height: 100), in: area, margin: 8)
        let tall = PanelPlacement.frame(corner: .bottomRight, size: CGSize(width: 420, height: 700), in: area, margin: 8)
        XCTAssertEqual(short.minY, tall.minY)
        XCTAssertEqual(short.maxX, tall.maxX)

        let topShort = PanelPlacement.frame(corner: .topLeft, size: CGSize(width: 420, height: 100), in: area, margin: 8)
        let topTall = PanelPlacement.frame(corner: .topLeft, size: CGSize(width: 420, height: 700), in: area, margin: 8)
        XCTAssertEqual(topShort.maxY, topTall.maxY)
    }

    func testAPanelTallerThanTheScreenIsReducedToFit() {
        let frame = PanelPlacement.frame(corner: .bottomRight, size: CGSize(width: 420, height: 5000), in: area, margin: 8)
        XCTAssertEqual(frame.height, area.height - 16)
        XCTAssertTrue(area.contains(frame))
    }

    func testThePanelBelongsToTheScreenItOverlapsMost() {
        let areas = [CGRect(x: 0, y: 0, width: 2560, height: 1440), CGRect(x: 2560, y: 0, width: 2560, height: 1440)]
        XCTAssertEqual(PanelPlacement.screenIndex(for: CGRect(x: 2300, y: 100, width: 420, height: 300), in: areas), 0)
        XCTAssertEqual(PanelPlacement.screenIndex(for: CGRect(x: 2500, y: 100, width: 420, height: 300), in: areas), 1)
    }

    func testAPanelOutsideEveryScreenGoesToTheClosestOne() {
        let areas = [CGRect(x: 0, y: 0, width: 1000, height: 800), CGRect(x: 1000, y: 0, width: 1000, height: 800)]
        XCTAssertEqual(PanelPlacement.screenIndex(for: CGRect(x: 3000, y: 100, width: 100, height: 100), in: areas), 1)
        XCTAssertNil(PanelPlacement.screenIndex(for: .zero, in: []))
    }
}
