import { describe, expect, it } from 'vitest';
import { fileProblem, MAX_BYTES, MAX_FILES } from './composerFiles';

describe('fileProblem', () => {
  it('accepts images and text', () => {
    expect(fileProblem({ name: 'a.png', size: 10, type: 'image/png' }, 0)).toBeNull();
    expect(fileProblem({ name: 'a.log', size: 10, type: 'text/plain' }, 0)).toBeNull();
  });

  it('refuses too many, too large and unsupported files', () => {
    expect(fileProblem({ name: 'a.png', size: 10, type: 'image/png' }, MAX_FILES)).toMatch(/At most/);
    expect(fileProblem({ name: 'big.png', size: MAX_BYTES + 1, type: 'image/png' }, 0)).toMatch(/larger than 8 MB/);
    expect(fileProblem({ name: 'a.zip', size: 10, type: 'application/zip' }, 0)).toMatch(/only images and text/);
  });
});
