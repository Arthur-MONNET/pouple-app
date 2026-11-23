import { describe, expect, it } from 'vitest';
import { ApiError } from './client';
import { createMockRemote } from './mockRemote';

const mock = () => createMockRemote({ wait: () => Promise.resolve(), pairAfterMs: null });

describe('mock phone access', () => {
  it('refuses to turn on with no network and a port out of range, like the core', async () => {
    const remote = mock();
    await expect(remote.updateRemoteSettings({ enabled: true })).rejects.toBeInstanceOf(ApiError);
    await expect(remote.updateRemoteSettings({ port: 80 })).rejects.toMatchObject({ status: 422 });
    const on = await remote.updateRemoteSettings({ enabled: true, interfaces: ['en0'] });
    expect(on.listening[0]).toBe('192.168.1.24:47821');
  });

  it('opens a pairing window only when on, and revokes a device', async () => {
    const remote = mock();
    await expect(remote.openPairing()).rejects.toMatchObject({ status: 409 });
    await remote.updateRemoteSettings({ enabled: true, interfaces: ['en0'] });
    const offer = await remote.openPairing();
    expect(offer.payload.startsWith('pouple-pair:v1?hosts=')).toBe(true);
    const [first] = await remote.listRemoteDevices();
    await remote.revokeRemoteDevice(first!.id);
    expect((await remote.listRemoteDevices())[0]?.revoked).toBe(true);
    await expect(remote.revokeRemoteDevice('nope')).rejects.toMatchObject({ status: 404 });
  });

  it('keeps the access from outside off by default and validates it like the core', async () => {
    const remote = mock();
    const initial = await remote.getRemoteSettings();
    expect(initial).toMatchObject({ externalEnabled: false, externalHost: '', externalPort: 0, externalDetected: '', externalDetectedAt: '', externalReady: false });
    await expect(remote.updateRemoteSettings({ externalEnabled: true })).rejects.toMatchObject({ status: 422 });
    await expect(remote.updateRemoteSettings({ externalHost: 'https://home.example.net' })).rejects.toMatchObject({ status: 422 });
    await expect(remote.updateRemoteSettings({ externalPort: 65536 })).rejects.toMatchObject({ status: 422 });
    await expect(remote.updateRemoteSettings({ externalPort: -1 })).rejects.toMatchObject({ status: 422 });
    expect((await remote.getRemoteSettings()).externalEnabled).toBe(false);
    const on = await remote.updateRemoteSettings({ enabled: true, interfaces: ['en0'], externalEnabled: true, externalHost: ' Home.Example.NET ', externalPort: 8443 });
    expect(on).toMatchObject({ externalEnabled: true, externalHost: 'home.example.net', externalPort: 8443, externalReady: true });
  });

  it('adds ext last to the pairing payload, with an IPv6 address bracketed, only while the address is known', async () => {
    const remote = mock();
    await remote.updateRemoteSettings({ enabled: true, interfaces: ['en0'] });
    expect((await remote.openPairing()).payload).not.toContain('ext=');
    await remote.updateRemoteSettings({ externalEnabled: true, externalHost: 'home.example.net' });
    expect((await remote.openPairing()).payload.endsWith('&name=My-Mac&ext=home.example.net%3A47821')).toBe(true);
    await remote.updateRemoteSettings({ externalHost: '2001:db8::1', externalPort: 8443 });
    expect((await remote.openPairing()).payload.endsWith('&ext=%5B2001%3Adb8%3A%3A1%5D%3A8443')).toBe(true);
  });

  it('detects a public address for auto, and uses it in both payloads', async () => {
    const remote = mock();
    await remote.updateRemoteSettings({ enabled: true, interfaces: ['en0'], externalEnabled: true, externalHost: 'auto' });
    const view = await remote.getRemoteSettings();
    expect(view.externalDetected).toBe('203.0.113.50');
    expect(Number.isNaN(Date.parse(view.externalDetectedAt))).toBe(false);
    expect((await remote.openPairing()).payload.endsWith('&ext=203.0.113.50%3A47821')).toBe(true);
    const address = await remote.getRemoteAddress();
    expect(address.payload).toBe(`pouple-address:v1?ext=203.0.113.50%3A47821&fp=${view.fingerprint}`);
    // Not detecting once the access from outside is off.
    await remote.updateRemoteSettings({ externalEnabled: false });
    expect((await remote.getRemoteSettings()).externalDetected).toBe('');
  });

  it('refuses the address update while the access is off or no address is known', async () => {
    const remote = mock();
    await expect(remote.getRemoteAddress()).rejects.toMatchObject({ status: 409 });
    await remote.updateRemoteSettings({ enabled: true, interfaces: ['en0'] });
    await expect(remote.getRemoteAddress()).rejects.toMatchObject({ status: 409 });
    await remote.updateRemoteSettings({ externalEnabled: true, externalHost: 'home.example.net', externalPort: 443 });
    expect((await remote.getRemoteAddress()).payload).toMatch(/^pouple-address:v1\?ext=home\.example\.net%3A443&fp=[0-9a-f]{64}$/);
  });
});
