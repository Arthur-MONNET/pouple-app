import XCTest
@testable import PoupleCore

final class AttachmentsTests: XCTestCase {
    func testAMessageTakesImagesAndTextFilesOfAReasonableSize() {
        XCTAssertNil(AttachmentRules.problem(name: "Screenshot 2026-11-13 at 12.51.01.png", size: 120_000, alreadyAdded: 0))
        XCTAssertNil(AttachmentRules.problem(name: "photo.JPEG", size: 5 << 20, alreadyAdded: 5))
        XCTAssertNil(AttachmentRules.problem(name: "error.log", size: 10, alreadyAdded: 0))
        XCTAssertNil(AttachmentRules.problem(name: "notes.md", size: AttachmentRules.maxBytes, alreadyAdded: 0))
    }

    func testEachRefusalSaysWhy() {
        XCTAssertEqual(AttachmentRules.problem(name: "a.png", size: 10, alreadyAdded: AttachmentRules.maxFiles), .tooMany)
        XCTAssertEqual(AttachmentRules.problem(name: "a.png", size: AttachmentRules.maxBytes + 1, alreadyAdded: 0), .tooLarge)
        XCTAssertEqual(AttachmentRules.problem(name: "a.png", size: 0, alreadyAdded: 0), .empty)
        XCTAssertEqual(AttachmentRules.problem(name: "archive.zip", size: 10, alreadyAdded: 0), .unsupported)
        XCTAssertEqual(AttachmentRules.problem(name: "no-extension", size: 10, alreadyAdded: 0), .unsupported)
    }

    func testTheRulesAreTheOnesOfTheWebWindow() {
        // web/src/composables/composerFiles.ts: 6 files, 8 MB each.
        XCTAssertEqual(AttachmentRules.maxFiles, 6)
        XCTAssertEqual(AttachmentRules.maxBytes, 8 * 1024 * 1024)
    }

    func testTheRefusalsAreWordedInBothLanguages() {
        for language in [Language.fr, .en] {
            XCTAssertTrue(L10n.text(.attachmentTooLarge, "a.png", language: language).contains("a.png"))
            XCTAssertTrue(L10n.text(.attachmentTooMany, 6, language: language).contains("6"))
        }
    }
}
