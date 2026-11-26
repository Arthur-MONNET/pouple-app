import { describe, expect, it } from 'vitest';
import { isAway, snoozeUntil, undoOf } from './shelf';

describe('snooze presets', () => {
  it('is an hour later for "in an hour"', () => {
    const now = new Date(2026, 10, 26, 16, 30);
    expect(new Date(snoozeUntil('hour', now)).getTime() - now.getTime()).toBe(3_600_000);
  });

  it('lands tomorrow at 9:00 local time', () => {
    const target = new Date(snoozeUntil('tomorrow', new Date(2026, 10, 26, 16, 30)));
    expect([target.getFullYear(), target.getMonth(), target.getDate(), target.getHours(), target.getMinutes()]).toEqual([2026, 10, 27, 9, 0]);
  });

  it('lands on the next Monday at 9:00, a week later when it is Monday already', () => {
    const thursday = new Date(snoozeUntil('week', new Date(2026, 10, 26, 16, 30)));
    expect([thursday.getDate(), thursday.getDay(), thursday.getHours()]).toEqual([30, 1, 9]);
    const monday = new Date(snoozeUntil('week', new Date(2026, 10, 30, 8, 0)));
    expect([monday.getMonth(), monday.getDate(), monday.getDay()]).toEqual([11, 7, 1]);
  });
});

describe('undoing an arrangement', () => {
  const now = new Date('2026-11-26T10:00:00Z');

  it('restores a pin that archiving removed', () => {
    expect(undoOf({ pinnedAt: '2026-11-20T10:00:00Z' }, now)).toEqual({ pinned: true, archived: false, snoozedUntil: '' });
  });

  it('restores a snooze that is still ahead', () => {
    expect(undoOf({ snoozedUntil: '2026-11-27T09:00:00Z' }, now)).toEqual({ pinned: false, archived: false, snoozedUntil: '2026-11-27T09:00:00Z' });
  });

  it('does not ask again for a snooze that has passed', () => {
    expect(undoOf({ snoozedUntil: '2026-11-25T09:00:00Z' }, now).snoozedUntil).toBe('');
  });

  it('restores an archive', () => {
    expect(undoOf({ archivedAt: '2026-11-20T10:00:00Z' }, now)).toEqual({ pinned: false, archived: true, snoozedUntil: '' });
  });
});

describe('what is away', () => {
  it('counts archived and snoozed conversations as away', () => {
    expect([{}, { archivedAt: 'x' }, { snoozedUntil: 'y' }].map(isAway)).toEqual([false, true, true]);
  });
});
