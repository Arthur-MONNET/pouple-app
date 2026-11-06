import Foundation

public enum MarkdownBlock: Equatable, Sendable {
    case paragraph(String)
    case heading(String)
    case list(ordered: Bool, items: [String])
    case code(String)
}

/// The small Markdown subset the assistant writes: paragraphs, headings, lists, code blocks, and inline emphasis.
public enum Markdown {
    private static let fence = try! NSRegularExpression(pattern: #"^\s*```"#)
    private static let heading = try! NSRegularExpression(pattern: #"^\s{0,3}#{1,6}\s+(.*)$"#)
    private static let bullet = try! NSRegularExpression(pattern: #"^\s*[-*•]\s+(.*)$"#)
    private static let number = try! NSRegularExpression(pattern: #"^\s*\d+[.)]\s+(.*)$"#)

    public static func blocks(_ source: String) -> [MarkdownBlock] {
        var blocks: [MarkdownBlock] = []
        var paragraph: [String] = []
        var list: (ordered: Bool, items: [String])?
        let lines = source.replacingOccurrences(of: "\r\n", with: "\n").components(separatedBy: "\n")

        func flushParagraph() {
            if !paragraph.isEmpty { blocks.append(.paragraph(paragraph.joined(separator: "\n"))) }
            paragraph = []
        }
        func flushList() {
            if let list { blocks.append(.list(ordered: list.ordered, items: list.items)) }
            list = nil
        }

        var index = 0
        while index < lines.count {
            let line = lines[index]
            index += 1
            if matches(fence, line) != nil {
                flushParagraph()
                flushList()
                var code: [String] = []
                while index < lines.count, matches(fence, lines[index]) == nil {
                    code.append(lines[index])
                    index += 1
                }
                index += 1
                blocks.append(.code(code.joined(separator: "\n")))
                continue
            }
            let bulletText = matches(bullet, line)
            let numberText = bulletText == nil ? matches(number, line) : nil
            if let item = bulletText ?? numberText {
                flushParagraph()
                let ordered = numberText != nil
                if let current = list, current.ordered != ordered { flushList() }
                list = (ordered, (list?.items ?? []) + [item])
                continue
            }
            if let title = matches(heading, line) {
                flushParagraph()
                flushList()
                blocks.append(.heading(title))
                continue
            }
            if line.trimmingCharacters(in: .whitespaces).isEmpty {
                flushParagraph()
                flushList()
                continue
            }
            flushList()
            paragraph.append(line)
        }
        flushParagraph()
        flushList()
        return blocks
    }

    /// Inline Markdown as styled text. Only web links stay links: a `file:` or custom-scheme link written by someone
    /// else must not become clickable.
    public static func inline(_ source: String) -> AttributedString {
        let options = AttributedString.MarkdownParsingOptions(interpretedSyntax: .inlineOnlyPreservingWhitespace)
        guard var text = try? AttributedString(markdown: source, options: options) else { return AttributedString(source) }
        for run in text.runs {
            guard let link = run.link else { continue }
            if link.scheme?.lowercased() != "http" && link.scheme?.lowercased() != "https" {
                text[run.range].link = nil
            }
        }
        return text
    }

    /// The first capture group when `line` matches, else nil.
    private static func matches(_ expression: NSRegularExpression, _ line: String) -> String? {
        let range = NSRange(line.startIndex..., in: line)
        guard let match = expression.firstMatch(in: line, range: range) else { return nil }
        guard match.numberOfRanges > 1, let capture = Range(match.range(at: 1), in: line) else { return "" }
        return String(line[capture])
    }
}
