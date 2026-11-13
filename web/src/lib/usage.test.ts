import { describe, expect, it } from 'vitest';
import { compactCount, percent, untilTime } from './format';
import { totalTokens, usageLevel, usageRows } from './usage';

const seenAt = '2026-11-13T12:00:00.000Z';

describe('usage by AI', () => {
  it('keeps each AI with its own tokens and windows, by name, five hours before the week', () => {
    const rows = usageRows({
      today: 3,
      limit: null,
      tokensByAI: { codex: { input: 1_500_000, cached: 1_200_000, output: 8_000 } },
      limits: {
        codex: { week: { usedPercent: 27 }, fiveHour: { usedPercent: 9 }, seenAt },
        claude: { week: { usedPercent: 60 }, seenAt },
      },
    });
    expect(rows.map((row) => row.name)).toEqual(['Claude', 'Codex']);
    // Claude used no tokens today: the ones of Codex are never shown as its.
    expect(rows[0]?.tokens).toBeUndefined();
    expect(rows[1]?.tokens?.input).toBe(1_500_000);
    expect(rows[1]?.windows.map((window) => window.key)).toEqual(['fiveHour', 'week']);
  });

  it('shows an AI that used tokens even before it reported its windows', () => {
    const rows = usageRows({ today: 1, limit: null, tokensByAI: { claude: { input: 10, cached: 0, output: 2 } } });
    expect(rows).toEqual([{ provider: 'claude', name: 'Claude', tokens: { input: 10, cached: 0, output: 2 }, windows: [] }]);
  });

  it('shows nothing for a core that sends no usage, or none left', () => {
    expect(usageRows(null)).toEqual([]);
    expect(usageRows({ today: 0, limit: null })).toEqual([]);
    expect(usageRows({ today: 0, limit: null, limits: { codex: { seenAt } }, tokensByAI: { codex: { input: 0, cached: 0, output: 0 } } })).toEqual([]);
  });

  it('leaves out a window that started again since it was reported', () => {
    const now = new Date('2026-11-13T12:00:00Z');
    const rows = usageRows(
      { today: 0, limit: null, limits: { codex: { fiveHour: { usedPercent: 90, resetsAt: '2026-11-13T11:59:00Z' }, week: { usedPercent: 30, resetsAt: '2026-11-15T00:00:00Z' }, seenAt } } },
      now,
    );
    expect(rows[0]?.windows.map((window) => window.key)).toEqual(['week']);
  });

  it('turns high from 75 % and critical from 90 %', () => {
    expect(usageLevel({ usedPercent: 74 })).toBe('normal');
    expect(usageLevel({ usedPercent: 75 })).toBe('high');
    expect(usageLevel({ usedPercent: 89 })).toBe('high');
    expect(usageLevel({ usedPercent: 90 })).toBe('critical');
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
