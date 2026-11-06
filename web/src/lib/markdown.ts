/** A deliberately small Markdown subset (what the assistant writes), parsed into data so no HTML is ever injected. */

export type Inline =
  | { type: 'text'; text: string }
  | { type: 'bold' | 'italic' | 'code'; text: string }
  | { type: 'link'; text: string; href: string };

export type Block =
  | { type: 'paragraph'; inlines: Inline[] }
  | { type: 'heading'; inlines: Inline[] }
  | { type: 'list'; ordered: boolean; items: Inline[][] }
  | { type: 'code'; text: string };

// Order matters: code first so its content is never read as emphasis.
const INLINE_RE = /`([^`\n]+)`|\*\*([^*\n]+)\*\*|__([^_\n]+)__|\*([^*\s][^*\n]*?)\*|\[([^\]\n]+)\]\((https?:\/\/[^\s)]+)\)|(https?:\/\/[^\s<>()]+[^\s<>().,;:!?])/g;

export function parseInline(source: string): Inline[] {
  const result: Inline[] = [];
  let last = 0;
  for (const match of source.matchAll(INLINE_RE)) {
    if (match.index > last) result.push({ type: 'text', text: source.slice(last, match.index) });
    const [, code, bold, boldUnderscore, italic, label, href, bare] = match;
    if (code !== undefined) result.push({ type: 'code', text: code });
    else if (bold !== undefined || boldUnderscore !== undefined) result.push({ type: 'bold', text: (bold ?? boldUnderscore) as string });
    else if (italic !== undefined) result.push({ type: 'italic', text: italic });
    else if (label !== undefined && href !== undefined) result.push({ type: 'link', text: label, href });
    else if (bare !== undefined) result.push({ type: 'link', text: bare, href: bare });
    last = match.index + match[0].length;
  }
  if (last < source.length) result.push({ type: 'text', text: source.slice(last) });
  return result;
}

const FENCE_RE = /^\s*```/;
const HEADING_RE = /^\s{0,3}#{1,6}\s+(.*)$/;
const BULLET_RE = /^\s*[-*•]\s+(.*)$/;
const NUMBER_RE = /^\s*\d+[.)]\s+(.*)$/;

export function parseMarkdown(source: string): Block[] {
  const blocks: Block[] = [];
  const lines = source.replace(/\r\n?/g, '\n').split('\n');
  let paragraph: string[] = [];
  let list: { ordered: boolean; items: Inline[][] } | null = null;

  const flushParagraph = (): void => {
    if (paragraph.length > 0) blocks.push({ type: 'paragraph', inlines: parseInline(paragraph.join('\n')) });
    paragraph = [];
  };
  const flushList = (): void => {
    if (list) blocks.push({ type: 'list', ...list });
    list = null;
  };

  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index] as string;
    if (FENCE_RE.test(line)) {
      flushParagraph();
      flushList();
      const code: string[] = [];
      index += 1;
      while (index < lines.length && !FENCE_RE.test(lines[index] as string)) code.push(lines[index++] as string);
      blocks.push({ type: 'code', text: code.join('\n') });
      continue;
    }
    const bullet = BULLET_RE.exec(line);
    const number = bullet ? null : NUMBER_RE.exec(line);
    const item = bullet ?? number;
    if (item) {
      flushParagraph();
      const ordered = number !== null;
      if (list && list.ordered !== ordered) flushList();
      list ??= { ordered, items: [] };
      list.items.push(parseInline(item[1] as string));
      continue;
    }
    const heading = HEADING_RE.exec(line);
    if (heading) {
      flushParagraph();
      flushList();
      blocks.push({ type: 'heading', inlines: parseInline(heading[1] as string) });
      continue;
    }
    if (line.trim() === '') {
      flushParagraph();
      flushList();
      continue;
    }
    flushList();
    paragraph.push(line);
  }
  flushParagraph();
  flushList();
  return blocks;
}
