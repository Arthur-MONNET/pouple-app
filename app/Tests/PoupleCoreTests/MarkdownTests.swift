import XCTest
@testable import PoupleCore

final class MarkdownTests: XCTestCase {
    func testSplitsParagraphsListsHeadingsAndCode() {
        let blocks = Markdown.blocks("# Title\nIntro:\n- **Vision**: feed\n- Pay-ui\n\n1. one\n2. two\n\n```\nlet a = 1\n```\nend")
        XCTAssertEqual(blocks, [
            .heading("Title"),
            .paragraph("Intro:"),
            .list(ordered: false, items: ["**Vision**: feed", "Pay-ui"]),
            .list(ordered: true, items: ["one", "two"]),
            .code("let a = 1"),
            .paragraph("end"),
        ])
    }

    func testKeepsLineBreaksInsideAParagraph() {
        XCTAssertEqual(Markdown.blocks("a\nb"), [.paragraph("a\nb")])
    }

    func testAnUnclosedCodeFenceDoesNotLoseText() {
        XCTAssertEqual(Markdown.blocks("```\nlet a = 1"), [.code("let a = 1")])
    }

    func testInlineEmphasisBecomesStyling() {
        let text = Markdown.inline("a **b** `c`")
        XCTAssertEqual(String(text.characters), "a b c")
        XCTAssertTrue(text.runs.contains { $0.inlinePresentationIntent?.contains(.stronglyEmphasized) == true })
        XCTAssertTrue(text.runs.contains { $0.inlinePresentationIntent?.contains(.code) == true })
    }

    func testOnlyWebLinksStayClickable() {
        let web = Markdown.inline("[docs](https://example.com/a)")
        XCTAssertEqual(web.runs.compactMap(\.link).map(\.absoluteString), ["https://example.com/a"])

        for risky in ["[x](file:///etc/passwd)", "[x](javascript:alert(1))", "[x](pouple://run)"] {
            XCTAssertTrue(Markdown.inline(risky).runs.allSatisfy { $0.link == nil }, risky)
        }
    }

    func testTextThatIsNotValidMarkdownStillShows() {
        XCTAssertEqual(String(Markdown.inline("5 * 3 = 15 [oops").characters), "5 * 3 = 15 [oops")
    }
}
