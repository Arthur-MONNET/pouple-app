import { describe, expect, it } from 'vitest';
import { qrMatrix } from './pairingQr';

describe('qrMatrix', () => {
  it('adds the quiet zone around a version 1 code', () => {
    const { size } = qrMatrix('hi');
    expect(size).toBe(21 + 8);
  });

  it('grows with the text and keeps modules inside the grid', () => {
    const payload = `pouple-pair:v1?hosts=192.168.1.24,Mac.local&port=47821&fp=${'ab'.repeat(32)}&code=${'cd'.repeat(16)}&name=Mac`;
    const { size, path } = qrMatrix(payload);
    expect(size).toBeGreaterThan(29);
    const coordinates = [...path.matchAll(/M(\d+) (\d+)h1v1h-1z/g)].map((m) => [Number(m[1]), Number(m[2])] as const);
    expect(coordinates.length).toBeGreaterThan(100);
    for (const [x, y] of coordinates) {
      expect(x).toBeGreaterThanOrEqual(4);
      expect(y).toBeGreaterThanOrEqual(4);
      expect(x).toBeLessThan(size - 4);
      expect(y).toBeLessThan(size - 4);
    }
  });

  it('is deterministic', () => {
    expect(qrMatrix('same')).toEqual(qrMatrix('same'));
  });
});
