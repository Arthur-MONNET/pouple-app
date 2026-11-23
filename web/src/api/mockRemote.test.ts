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
});
