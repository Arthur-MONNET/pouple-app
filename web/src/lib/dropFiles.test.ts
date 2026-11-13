import { describe, expect, it } from 'vitest';
import { hasFiles } from './dropFiles';

const dragWith = (types: string[]): DragEvent => ({ dataTransfer: { types } }) as unknown as DragEvent;

describe('a drag over the conversation', () => {
  it('is for the conversation when it carries files', () => {
    expect(hasFiles(dragWith(['Files']))).toBe(true);
  });

  it('is not when it carries only text or a link', () => {
    expect(hasFiles(dragWith(['text/plain', 'text/uri-list']))).toBe(false);
    expect(hasFiles({} as DragEvent)).toBe(false);
  });
});
