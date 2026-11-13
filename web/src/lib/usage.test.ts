import { describe, expect, it } from 'vitest';
import { compactCount, percent, untilTime } from './format';
import { limitRows, nearlyUsed, totalTokens } from './usage';

describe('usage windows', () => {
  it('list the AIs that reported them, by name, five hours before the week', () => {
    const rows = limitRows({
      today: 3,
      limit: null,
      limits: {
        codex: { week: { usedPercent: 27 }, fiveHour: { usedPercent: 9 }, seenAt: '2026-11-13T12:00:00.000Z' },
        claude: { week: { usedPercent: 60 }, seenAt: '2026-11-13T12:00:00.000Z' },
      },
    });
    expect(rows.map((row) => row.name)).toEqual(['Claude', 'Codex']);
    expect(rows[1]?.windows.map((window) => window.key)).toEqual(['fiveHour', 'week']);
    expect(rows[0]?.windows.map((window) => window.key)).toEqual(['week']);
  });

  it('show nothing for a core that sends no windows, or none left', () => {
    expect(limitRows(null)).toEqual([]);
    expect(limitRows({ today: 0, limit: null })).toEqual([]);
    expect(limitRows({ today: 0, limit: null, limits: { codex: { seenAt: 'x' } } })).toEqual([]);
  });

  it('leave out a window that started again since it was reported', () => {
    const now = new Date('2026-11-13T12:00:00Z');
    const rows = limitRows(
      { today: 0, limit: null, limits: { codex: { fiveHour: { usedPercent: 90, resetsAt: '2026-11-13T11:59:00Z' }, week: { usedPercent: 30, resetsAt: '2026-11-15T00:00:00Z' }, seenAt: 'x' } } },
      now,
    );
    expect(rows[0]?.windows.map((window) => window.key)).toEqual(['week']);
  });

  it('warn from 80 %', () => {
    expect(nearlyUsed({ usedPercent: 79 })).toBe(false);
    expect(nearlyUsed({ usedPercent: 80 })).toBe(true);
  });
});

describe('counts', () => {
  it('add what was read and written', () => {
    expect(totalTokens({ input: 1000, cached: 800, output: 50 })).toBe(1050);
  });

  it('say when a window starts again, in the future', () => {
    const now = new Date('2026-11-13T12:00:00Z');
    expect(untilTime('2026-11-13T15:00:00Z', now, 'en')).toMatch(/^in 3 hr\.?$/);
    expect(untilTime('2026-11-15T12:00:00Z', now, 'en')).toBe('in 2 days');
  });

  it('are short and follow the language', () => {
    expect(compactCount(1250, 'en')).toMatch(/^1\.3k$/i);
    expect(compactCount(3_400_000, 'en')).toMatch(/^3\.4m$/i);
    expect(compactCount(1250, 'fr')).toMatch(/^1,3\s?k$/);
    expect(percent(9, 'en')).toBe('9%');
    expect(percent(9, 'fr')).toMatch(/^9\s%$/);
  });
});
