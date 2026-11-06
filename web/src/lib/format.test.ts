import { describe, expect, it } from 'vitest';
import { initials, relativeTime, sizeLabel } from './format';

const now = new Date('2026-11-06T12:00:00Z');
const ago = (ms: number): string => new Date(now.getTime() - ms).toISOString();

describe('relativeTime', () => {
  it('reads recent times in words', () => {
    expect(relativeTime(ago(10_000), now)).toBe('now');
    expect(relativeTime(ago(5 * 60_000), now)).toBe('5 min ago');
    expect(relativeTime(ago(3 * 3_600_000), now)).toBe('3 h ago');
    expect(relativeTime(ago(26 * 3_600_000), now)).toBe('yesterday');
  });

  it('falls back to the date, and to nothing for garbage', () => {
    expect(relativeTime(ago(10 * 86_400_000), now)).toMatch(/^[A-Z][a-z]{2} \d{1,2}$/);
    expect(relativeTime('not a date', now)).toBe('');
  });
});

describe('initials and sizes', () => {
  it('takes up to two initials', () => {
    expect(initials('Arthur Monnet')).toBe('AM');
    expect(initials('arthur@acme.example')).toBe('AA');
    expect(initials('')).toBe('?');
  });

  it('formats file sizes', () => {
    expect(sizeLabel(100)).toBe('1 KB');
    expect(sizeLabel(2.5 * 1024 * 1024)).toBe('2.5 MB');
  });
});
