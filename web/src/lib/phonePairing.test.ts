import { describe, expect, it } from 'vitest';
import type { RemoteDevice } from '../api/types';
import {
  defaultInterfaces,
  deviceStatus,
  formatCountdown,
  freshDevices,
  interfaceChoices,
  isValidPort,
  secondsLeft,
  toggleInterface,
  windowShare,
} from './phonePairing';

const device = (id: string, patch: Partial<RemoteDevice> = {}): RemoteDevice => ({ id, name: id, createdAt: '2026-11-01T10:00:00Z', lastSeen: '2026-11-01T10:00:00Z', revoked: false, expired: false, ...patch });

describe('pairing countdown', () => {
  const end = '2026-11-23T10:02:00Z';
  const at = (iso: string): number => new Date(iso).getTime();

  it('counts whole seconds up, so 0 means closed', () => {
    expect(secondsLeft(end, at('2026-11-23T10:00:00Z'))).toBe(120);
    expect(secondsLeft(end, at('2026-11-23T10:01:59.200Z'))).toBe(1);
    expect(secondsLeft(end, at('2026-11-23T10:02:00Z'))).toBe(0);
    expect(secondsLeft(end, at('2026-11-23T10:05:00Z'))).toBe(0);
  });

  it('treats a date it cannot read as closed', () => {
    expect(secondsLeft('soon', Date.now())).toBe(0);
  });

  it('formats minutes and padded seconds', () => {
    expect(formatCountdown(120)).toBe('2:00');
    expect(formatCountdown(119)).toBe('1:59');
    expect(formatCountdown(5)).toBe('0:05');
    expect(formatCountdown(0)).toBe('0:00');
    expect(formatCountdown(-3)).toBe('0:00');
  });

  it('gives the share of the window left, between 0 and 1', () => {
    expect(windowShare(120)).toBe(1);
    expect(windowShare(60)).toBe(0.5);
    expect(windowShare(500)).toBe(1);
    expect(windowShare(-1)).toBe(0);
  });
});

describe('port', () => {
  it('accepts whole numbers from 1024 to 65535 only', () => {
    expect([1024, 47821, 65535].every(isValidPort)).toBe(true);
    expect([0, 1023, 65536, 80.5, Number.NaN].some(isValidPort)).toBe(false);
  });
});

describe('devices', () => {
  it('finds the phones that appeared since the window opened, never a revoked one', () => {
    const known = new Set(['a']);
    expect(freshDevices(known, [device('a'), device('b'), device('c', { revoked: true })]).map((item) => item.id)).toEqual(['b']);
    expect(freshDevices(known, [device('a')])).toEqual([]);
  });

  it('says revoked before expired', () => {
    expect(deviceStatus({ revoked: true, expired: true })).toBe('revoked');
    expect(deviceStatus({ revoked: false, expired: true })).toBe('expired');
    expect(deviceStatus({ revoked: false, expired: false })).toBe('active');
  });
});

describe('networks', () => {
  const available = [
    { name: 'lo0', addresses: ['127.0.0.1'], loopback: true },
    { name: 'awdl0', addresses: [], loopback: false },
    { name: 'en0', addresses: ['192.168.1.24'], loopback: false },
  ];

  it('defaults to the first real network with an address, never the loopback', () => {
    expect(defaultInterfaces(available)).toEqual(['en0']);
    expect(defaultInterfaces([available[0]!])).toEqual([]);
  });

  it('keeps a chosen network that went away visible, marked missing', () => {
    const choices = interfaceChoices({ available, interfaces: ['en0', 'en7'] });
    expect(choices.map((item) => [item.name, item.selected, item.missing])).toEqual([
      ['lo0', false, false],
      ['awdl0', false, false],
      ['en0', true, false],
      ['en7', true, true],
    ]);
  });

  it('toggles one network without touching the others or duplicating it', () => {
    expect(toggleInterface(['en0'], 'en1', true)).toEqual(['en0', 'en1']);
    expect(toggleInterface(['en0', 'en1'], 'en0', false)).toEqual(['en1']);
    expect(toggleInterface(['en0'], 'en0', true)).toEqual(['en0']);
  });
});
