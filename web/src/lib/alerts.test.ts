import { describe, expect, it } from 'vitest';
import type { AssistantAlert } from '../api/types';
import { formatClock, groupByDay, isUpcoming, nextHour, toLocalInput } from './alerts';

// Dates are built from local fields: the tests mean the same thing in any time zone.
const local = (month: number, day: number, hour = 0, minute = 0): Date => new Date(2026, month - 1, day, hour, minute);
const alert = (id: string, at: Date): AssistantAlert => ({ id, title: id, at: at.toISOString(), createdAt: local(11, 1).toISOString() });

describe('toLocalInput', () => {
  it('writes the local date and time to the minute, as a datetime-local input wants it', () => {
    expect(toLocalInput(local(11, 20, 9, 5).toISOString())).toBe('2026-11-20T09:05');
  });

  it('gives an empty value for something that is not a date', () => {
    expect(toLocalInput('soon')).toBe('');
  });
});

describe('isUpcoming', () => {
  const now = local(11, 16, 10, 0);

  it('accepts a complete local time after now', () => {
    expect(isUpcoming('2026-11-16T10:01', now)).toBe(true);
    expect(isUpcoming('2026-12-01T09:00', now)).toBe(true);
  });

  it('refuses now, the past, and what is incomplete', () => {
    expect(isUpcoming('2026-11-16T10:00', now)).toBe(false);
    expect(isUpcoming('2026-11-15T23:59', now)).toBe(false);
    expect(isUpcoming('', now)).toBe(false);
    expect(isUpcoming('2026-11-20', now)).toBe(false);
  });
});

describe('nextHour', () => {
  it('starts a new alert on the next whole hour, at least half an hour ahead', () => {
    expect(nextHour(local(11, 16, 10, 5))).toBe('2026-11-16T11:00');
    expect(nextHour(local(11, 16, 10, 40))).toBe('2026-11-16T12:00');
    expect(nextHour(local(11, 16, 23, 50))).toBe('2026-11-17T01:00');
  });
});

describe('formatClock', () => {
  it('shows the time of day, in 24 hours', () => {
    expect(formatClock(local(11, 20, 9, 5).toISOString(), 'fr')).toBe('09:05');
    expect(formatClock(local(11, 20, 17, 30).toISOString(), 'en')).toBe('17:30');
  });
});

describe('groupByDay', () => {
  const now = local(11, 16, 10, 0);

  it('names today and tomorrow, then each later day, soonest first', () => {
    const days = groupByDay(
      [alert('late', local(11, 20, 9)), alert('soon', local(11, 16, 15)), alert('next', local(11, 17, 9)), alert('earlier-today', local(11, 16, 11))],
      now,
      'en',
    );
    expect(days.map((day) => day.label)).toEqual(['Today', 'Tomorrow', 'Friday 20 November']);
    expect(days.map((day) => day.alerts.map((a) => a.id))).toEqual([['earlier-today', 'soon'], ['next'], ['late']]);
  });

  it('keeps the day a stable key, whatever the language', () => {
    expect(groupByDay([alert('a', local(11, 20, 9))], now, 'fr')[0]?.key).toBe('2026-11-20');
  });

  it('has no day for no alert', () => {
    expect(groupByDay([], now)).toEqual([]);
  });
});
