import { t } from '../i18n';

/**
 * Server-sent events over fetch. EventSource cannot send an Authorization header nor a POST body, so the answer body
 * is read as a stream and cut into frames here: `data: {json}` lines closed by a blank line.
 */

/** Cuts a stream of text chunks into the `data` payload of each complete frame. */
export function createSseParser(onData: (data: string) => void): { push: (chunk: string) => void; flush: () => void } {
  let buffer = '';

  const emitFrame = (frame: string): void => {
    const data: string[] = [];
    for (const line of frame.split('\n')) {
      // A line starting with ":" is a comment (keep-alive); other fields (event, id, retry) are not used.
      if (line.startsWith('data:')) data.push(line.slice(5).replace(/^ /, ''));
    }
    if (data.length > 0) onData(data.join('\n'));
  };

  return {
    push(chunk) {
      buffer += chunk.replace(/\r\n?/g, '\n');
      let boundary = buffer.indexOf('\n\n');
      while (boundary !== -1) {
        emitFrame(buffer.slice(0, boundary));
        buffer = buffer.slice(boundary + 2);
        boundary = buffer.indexOf('\n\n');
      }
    },
    flush() {
      if (buffer.trim() !== '') emitFrame(buffer);
      buffer = '';
    },
  };
}

/** Reads a streamed response to its end, calling onData with each frame's payload. */
export async function readSse(response: Response, onData: (data: string) => void): Promise<void> {
  if (!response.body) throw new Error(t('errors.noStream'));
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  const parser = createSseParser(onData);
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      parser.push(decoder.decode(value, { stream: true }));
    }
    parser.push(decoder.decode());
    parser.flush();
  } finally {
    reader.releaseLock();
  }
}
