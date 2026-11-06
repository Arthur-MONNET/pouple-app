import { describe, expect, it } from 'vitest';
import { createSseParser, readSse } from './sse';

const collect = (chunks: string[]): string[] => {
  const out: string[] = [];
  const parser = createSseParser((data) => out.push(data));
  for (const chunk of chunks) parser.push(chunk);
  parser.flush();
  return out;
};

describe('createSseParser', () => {
  it('emits one payload per blank-line-terminated frame', () => {
    expect(collect(['data: {"a":1}\n\ndata: {"a":2}\n\n'])).toEqual(['{"a":1}', '{"a":2}']);
  });

  it('reassembles a frame split across chunks, even in the middle of the delimiter', () => {
    expect(collect(['data: {"type":"lo', 'g"}\n', '\ndata: two\n\n'])).toEqual(['{"type":"log"}', 'two']);
  });

  it('understands CRLF line endings', () => {
    expect(collect(['data: one\r\n\r\ndata: two\r\n\r\n'])).toEqual(['one', 'two']);
  });

  it('ignores comments and fields other than data', () => {
    expect(collect([': keep-alive\n\nevent: update\nid: 7\ndata: payload\n\n'])).toEqual(['payload']);
  });

  it('joins several data lines of one frame with a newline', () => {
    expect(collect(['data: a\ndata: b\n\n'])).toEqual(['a\nb']);
  });

  it('flushes a final frame the server did not terminate', () => {
    expect(collect(['data: last\n'])).toEqual(['last']);
  });
});

describe('readSse', () => {
  it('reads a streamed response body to its end', async () => {
    const encoder = new TextEncoder();
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(encoder.encode('data: {"type":"log","text":"hé'));
        controller.enqueue(encoder.encode('llo"}\n\ndata: {"type":"done"}\n\n'));
        controller.close();
      },
    });
    const seen: string[] = [];
    await readSse(new Response(body), (data) => seen.push(data));
    expect(seen).toEqual(['{"type":"log","text":"héllo"}', '{"type":"done"}']);
  });
});
