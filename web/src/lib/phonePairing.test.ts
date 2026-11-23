import { describe, expect, it } from 'vitest';
import type { RemoteDevice } from '../api/types';
import {
  checkExternalHost,
  defaultInterfaces,
  deviceStatus,
  externalEndpoint,
  forwardedPort,
  formatCountdown,
  freshDevices,
  interfaceChoices,
  isValidPort,
  localAddress,
  parseExternalPort,
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

describe('external host', () => {
  const ok = (text: string, host: string) => expect(checkExternalHost(text)).toEqual({ ok: true, host });
  const bad = (text: string, reason: 'required' | 'invalid' = 'invalid') => expect(checkExternalHost(text)).toEqual({ ok: false, reason });

  it('accepts a DNS name, an IPv4 address, a bare IPv6 address and auto, in the canonical form the core stores', () => {
    ok('home.example.net', 'home.example.net');
    ok('  Home.Example.NET ', 'home.example.net');
    ok('my-mac-2.duckdns.org', 'my-mac-2.duckdns.org');
    ok('mymac', 'mymac');
    ok('203.0.113.7', '203.0.113.7');
    ok('10.8.0.1', '10.8.0.1');
    ok('2001:DB8::1', '2001:db8::1');
    ok('2001:0db8:0000:0000:0000:0000:0000:0001', '2001:db8::1');
    ok('AUTO', 'auto');
    ok('a'.repeat(63) + '.' + 'b'.repeat(63) + '.' + 'c'.repeat(63) + '.' + 'd'.repeat(61), `${'a'.repeat(63)}.${'b'.repeat(63)}.${'c'.repeat(63)}.${'d'.repeat(61)}`);
  });

  it('says required for an empty field', () => {
    bad('', 'required');
    bad('   ', 'required');
  });

  it('refuses what is not a bare name or address: scheme, path, port, brackets, spaces, zones, underscores', () => {
    for (const text of [
      'https://home.example.net',
      'home.example.net/x',
      'home.example.net:47821',
      '203.0.113.7:47821',
      '[2001:db8::1]',
      '[2001:db8::1]:47821',
      'fe80::1%en0',
      '::ffff:203.0.113.7',
      'home example.net',
      'me@home.example.net',
      'my_mac.example.net',
      'mönchen.example',
      '-home.example.net',
      'home-.example.net',
      'home..example.net',
      'home.example.net.',
      `${'a'.repeat(64)}.net`,
      'x'.repeat(254),
    ]) bad(text);
  });

  it('refuses malformed IPv4 and addresses nobody can reach from outside', () => {
    for (const text of ['999.1.1.1', '1.2.3', '01.2.3.4', '1.2.3.4.5', '0.0.0.0', '127.0.0.1', '169.254.1.1', '224.0.0.1', '::', '::1', 'fe80::1', 'ff02::1']) bad(text);
  });
});

describe('external port', () => {
  it('reads empty as "same port" and accepts 1 to 65535', () => {
    expect(parseExternalPort('')).toBe(0);
    expect(parseExternalPort('  ')).toBe(0);
    expect(parseExternalPort('0')).toBe(0);
    expect(parseExternalPort('443')).toBe(443);
    expect(parseExternalPort('65535')).toBe(65535);
  });

  it('refuses what is not a TCP port', () => {
    for (const text of ['65536', '-1', '80.5', 'abc', '1e3', '123456', '+80']) expect(parseExternalPort(text)).toBeNull();
  });

  it('forwards the external port, or the listening one when none is set', () => {
    expect(forwardedPort({ port: 47821, externalPort: 0 })).toBe(47821);
    expect(forwardedPort({ port: 47821, externalPort: 8443 })).toBe(8443);
  });
});

describe('external endpoint, as the pairing payload carries it', () => {
  const base = { port: 47821, externalEnabled: true, externalHost: 'home.example.net', externalPort: 0, externalDetected: '' };

  it('joins host and port, brackets an IPv6 address, and uses the external port when set', () => {
    expect(externalEndpoint(base)).toBe('home.example.net:47821');
    expect(externalEndpoint({ ...base, externalPort: 8443 })).toBe('home.example.net:8443');
    expect(externalEndpoint({ ...base, externalHost: '2001:db8::1' })).toBe('[2001:db8::1]:47821');
  });

  it('follows the detected address for auto, and is unknown until there is one', () => {
    expect(externalEndpoint({ ...base, externalHost: 'auto' })).toBeNull();
    expect(externalEndpoint({ ...base, externalHost: 'auto', externalDetected: '203.0.113.50' })).toBe('203.0.113.50:47821');
  });

  it('is unknown while the access from outside is off or no host is set', () => {
    expect(externalEndpoint({ ...base, externalEnabled: false })).toBeNull();
    expect(externalEndpoint({ ...base, externalHost: '' })).toBeNull();
  });
});

describe('local address to forward to', () => {
  it('is the first listening address that is not the loopback one, without brackets or port', () => {
    expect(localAddress(['127.0.0.1:47821', '[::1]:47821', '192.168.1.24:47821'])).toBe('192.168.1.24');
    expect(localAddress(['[2001:db8::7]:47821'])).toBe('2001:db8::7');
    expect(localAddress(['127.0.0.1:47821', '[::1]:47821'])).toBeNull();
    expect(localAddress([])).toBeNull();
  });
});
