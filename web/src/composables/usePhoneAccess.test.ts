import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { effectScope, nextTick } from 'vue';
import { ApiError, type PoupleApi } from '../api/client';
import type { PairingOffer, RemoteDevice, RemoteSettings, RemoteSettingsPatch } from '../api/types';
import { DEVICE_POLL_MS, TUNNEL_POLL_MS, usePhoneAccess } from './usePhoneAccess';

const settingsOf = (patch: Partial<RemoteSettings> = {}): RemoteSettings => ({
  enabled: true,
  interfaces: ['en0'],
  port: 47821,
  available: [{ name: 'en0', addresses: ['192.168.1.24'], loopback: false }],
  listening: ['192.168.1.24:47821'],
  problem: '',
  fingerprint: 'ab',
  externalEnabled: false,
  externalHost: '',
  externalPort: 0,
  externalDetected: '',
  externalDetectedAt: '',
  externalReady: false,
  tunnelEnabled: false,
  tunnelDomain: '',
  tunnelAuthtokenSet: false,
  tunnelStatus: 'off',
  tunnelProblem: '',
  ...patch,
});

const phone = (id: string, patch: Partial<RemoteDevice> = {}): RemoteDevice => ({ id, name: `Phone ${id}`, createdAt: '2026-11-01T10:00:00Z', lastSeen: '2026-11-01T10:00:00Z', revoked: false, expired: false, ...patch });

type Remote = Pick<PoupleApi, 'getRemoteSettings' | 'updateRemoteSettings' | 'getRemoteAddress' | 'openPairing' | 'cancelPairing' | 'listRemoteDevices' | 'revokeRemoteDevice'>;

function setup(overrides: Partial<Remote> = {}) {
  let devices: RemoteDevice[] = [phone('old')];
  let nextSettings = settingsOf();
  const api = {
    getRemoteSettings: vi.fn(() => Promise.resolve(nextSettings)),
    updateRemoteSettings: vi.fn((patch) => Promise.resolve((nextSettings = { ...nextSettings, ...patch }))),
    getRemoteAddress: vi.fn(() => Promise.resolve({ payload: 'pouple-address:v1?ext=home.example.net%3A47821&fp=ab' })),
    openPairing: vi.fn((): Promise<PairingOffer> => Promise.resolve({ payload: 'pouple-pair:v1?code=1', expiresAt: new Date(Date.now() + 120_000).toISOString() })),
    cancelPairing: vi.fn(() => Promise.resolve()),
    listRemoteDevices: vi.fn(() => Promise.resolve(structuredClone(devices))),
    revokeRemoteDevice: vi.fn(() => Promise.resolve()),
    ...overrides,
  } satisfies Remote;
  const scope = effectScope();
  const access = scope.run(() => usePhoneAccess(api as unknown as PoupleApi))!;
  return { api, access, scope, setDevices: (next: RemoteDevice[]) => (devices = next) };
}

describe('phone access', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-11-23T10:00:00Z'));
  });
  afterEach(() => vi.useRealTimers());

  it('loads the settings and the devices, and says why when it cannot', async () => {
    const { access } = setup();
    await access.load();
    expect(access.loading.value).toBe(false);
    expect(access.settings.value?.port).toBe(47821);
    expect(access.devices.value).toHaveLength(1);
    expect(access.canPair.value).toBe(true);

    const failing = setup({ getRemoteSettings: vi.fn(() => Promise.reject(new ApiError(500, 'Something went wrong'))) });
    await failing.access.load();
    expect(failing.access.loadError.value).toBe('Something went wrong');
    expect(failing.access.settings.value).toBeNull();
  });

  it('cannot pair while the access is off or not listening', async () => {
    const { access, api } = setup({ getRemoteSettings: vi.fn(() => Promise.resolve(settingsOf({ enabled: false, listening: [] }))) });
    await access.load();
    expect(access.canPair.value).toBe(false);
    await access.startPairing();
    expect(api.openPairing).not.toHaveBeenCalled();
    expect(access.phase.value).toBe('idle');
  });

  it('opens a window, counts down, and expires by itself', async () => {
    const { access, api } = setup();
    await access.load();
    await access.startPairing();
    expect(access.phase.value).toBe('open');
    expect(access.remaining.value).toBe(120);
    await vi.advanceTimersByTimeAsync(30_000);
    expect(access.remaining.value).toBe(90);
    await vi.advanceTimersByTimeAsync(90_000);
    expect(access.phase.value).toBe('expired');
    expect(access.remaining.value).toBe(0);
    const calls = vi.mocked(api.listRemoteDevices).mock.calls.length;
    await vi.advanceTimersByTimeAsync(20_000);
    // The poll stopped with the window.
    expect(vi.mocked(api.listRemoteDevices).mock.calls.length).toBe(calls);
  });

  it('ignores a second click while the window is opening or open', async () => {
    const { access, api } = setup();
    await access.load();
    const first = access.startPairing();
    await access.startPairing();
    await first;
    await access.startPairing();
    expect(api.openPairing).toHaveBeenCalledTimes(1);
  });

  it('shows the core message when the window cannot open', async () => {
    const { access } = setup({ openPairing: vi.fn(() => Promise.reject(new ApiError(409, 'Remote access is off.'))) });
    await access.load();
    await access.startPairing();
    expect(access.phase.value).toBe('idle');
    expect(access.pairingError.value).toBe('Remote access is off.');
    expect(access.offer.value).toBeNull();
  });

  it('refreshes the devices while the window is open and notices the phone that paired', async () => {
    const { access, setDevices } = setup();
    await access.load();
    await access.startPairing();
    setDevices([phone('new'), phone('old')]);
    await vi.advanceTimersByTimeAsync(DEVICE_POLL_MS);
    expect(access.phase.value).toBe('paired');
    expect(access.pairedName.value).toBe('Phone new');
    expect(access.devices.value?.map((item) => item.id)).toEqual(['new', 'old']);
  });

  it('cancels the window on the Mac and stops the timers', async () => {
    const { access, api } = setup();
    await access.load();
    await access.startPairing();
    await access.cancelPairing();
    expect(api.cancelPairing).toHaveBeenCalledTimes(1);
    expect(access.phase.value).toBe('idle');
    const calls = vi.mocked(api.listRemoteDevices).mock.calls.length;
    await vi.advanceTimersByTimeAsync(10_000);
    expect(vi.mocked(api.listRemoteDevices).mock.calls.length).toBe(calls);
  });

  it('closes a window whose opening finished after the cancel', async () => {
    let release: (offer: PairingOffer) => void = () => undefined;
    const { access, api } = setup({ openPairing: vi.fn(() => new Promise<PairingOffer>((resolve) => (release = resolve))) });
    await access.load();
    const starting = access.startPairing();
    await vi.advanceTimersByTimeAsync(0);
    await access.cancelPairing();
    release({ payload: 'p', expiresAt: new Date(Date.now() + 120_000).toISOString() });
    await starting;
    expect(access.phase.value).toBe('idle');
    expect(access.offer.value).toBeNull();
    // Once for the cancel, once for the window that opened late.
    expect(api.cancelPairing).toHaveBeenCalledTimes(2);
  });

  it('stops everything and closes the window when the screen goes away', async () => {
    const { access, api, scope } = setup();
    await access.load();
    await access.startPairing();
    scope.stop();
    expect(api.cancelPairing).toHaveBeenCalledTimes(1);
    const calls = vi.mocked(api.listRemoteDevices).mock.calls.length;
    await vi.advanceTimersByTimeAsync(10_000);
    expect(vi.mocked(api.listRemoteDevices).mock.calls.length).toBe(calls);
  });

  it('keeps the newest list when an older answer comes back late', async () => {
    const resolvers: ((devices: RemoteDevice[]) => void)[] = [];
    const { access } = setup({ listRemoteDevices: vi.fn(() => new Promise<RemoteDevice[]>((resolve) => resolvers.push(resolve))) });
    const first = access.refreshDevices();
    const second = access.refreshDevices();
    resolvers[1]?.([phone('fresh')]);
    await second;
    resolvers[0]?.([phone('stale')]);
    await first;
    expect(access.devices.value?.map((item) => item.id)).toEqual(['fresh']);
  });

  it('turns the access on with no network chosen: an empty list means automatic, and the port is not sent', async () => {
    const off = settingsOf({ enabled: false, interfaces: [], listening: [], port: 5000 });
    const update = vi.fn((patch: RemoteSettingsPatch) => Promise.resolve({ ...off, ...patch }));
    const { access } = setup({ getRemoteSettings: vi.fn(() => Promise.resolve(off)), updateRemoteSettings: update });
    await access.load();
    expect(await access.setAccess(true)).toBe(true);
    expect(update).toHaveBeenCalledWith({ enabled: true, interfaces: [] });
    expect(update.mock.calls[0]?.[0]).not.toHaveProperty('port');
  });

  it('keeps the networks chosen in the advanced section when the access is turned on again, and only sends enabled=false to turn it off', async () => {
    const off = settingsOf({ enabled: false, interfaces: ['en0'], listening: [] });
    const { access, api } = setup({ getRemoteSettings: vi.fn(() => Promise.resolve(off)) });
    await access.load();
    await access.setAccess(true);
    expect(api.updateRemoteSettings).toHaveBeenLastCalledWith({ enabled: true, interfaces: ['en0'] });
    await access.setAccess(false);
    expect(api.updateRemoteSettings).toHaveBeenLastCalledWith({ enabled: false });
  });

  it('asks the settings again while the tunnel is starting, and stops once it runs', async () => {
    const answers = [settingsOf({ tunnelEnabled: true, tunnelStatus: 'starting' }), settingsOf({ tunnelEnabled: true, tunnelStatus: 'running' })];
    const read = vi.fn(() => Promise.resolve(answers[Math.min(read.mock.calls.length - 1, answers.length - 1)]!));
    const { access, scope } = setup({ getRemoteSettings: read });
    await access.load();
    await nextTick();
    expect(access.settings.value?.tunnelStatus).toBe('starting');
    await vi.advanceTimersByTimeAsync(TUNNEL_POLL_MS);
    expect(access.settings.value?.tunnelStatus).toBe('running');
    const reads = read.mock.calls.length;
    await vi.advanceTimersByTimeAsync(TUNNEL_POLL_MS * 3);
    expect(read.mock.calls.length).toBe(reads);
    scope.stop();
  });

  it('sends one save at a time and keeps the core message of a refusal', async () => {
    let fail = true;
    const update = vi.fn(() => (fail ? Promise.reject(new ApiError(422, 'The port must be between 1024 and 65535.')) : Promise.resolve(settingsOf({ port: 5000 }))));
    const { access } = setup({ updateRemoteSettings: update });
    await access.load();
    const first = access.save({ port: 80 });
    expect(await access.save({ port: 81 })).toBe(false);
    expect(await first).toBe(false);
    expect(update).toHaveBeenCalledTimes(1);
    expect(access.saveError.value).toBe('The port must be between 1024 and 65535.');
    fail = false;
    expect(await access.save({ port: 5000 })).toBe(true);
    expect(access.saveError.value).toBeNull();
    expect(access.settings.value?.port).toBe(5000);
  });

  it('drops the open window when a setting changes: the listener restarted', async () => {
    const { access } = setup();
    await access.load();
    await access.startPairing();
    await access.save({ port: 5000 });
    expect(access.phase.value).toBe('idle');
    expect(access.offer.value).toBeNull();
  });

  it('revokes a device once, refreshes the list, and reports a refusal', async () => {
    let release: () => void = () => undefined;
    const revoke = vi.fn(() => new Promise<void>((resolve) => (release = resolve)));
    const { access } = setup({ revokeRemoteDevice: revoke });
    await access.load();
    const running = access.revoke('old');
    expect(access.revoking.value.has('old')).toBe(true);
    expect(await access.revoke('old')).toBe(false);
    release();
    expect(await running).toBe(true);
    expect(revoke).toHaveBeenCalledTimes(1);
    expect(access.revoking.value.has('old')).toBe(false);

    const refused = setup({ revokeRemoteDevice: vi.fn(() => Promise.reject(new ApiError(404, 'This device is not paired.'))) });
    await refused.access.load();
    expect(await refused.access.revoke('ghost')).toBe(false);
    expect(refused.access.revokeError.value).toBe('This device is not paired.');
  });

  describe('address update', () => {
    const external = { externalEnabled: true, externalHost: 'home.example.net' };

    it('can be shared only with the access from outside on, listening, and an address known', async () => {
      const cases: [Partial<RemoteSettings>, boolean][] = [
        [{}, false],
        [external, true],
        [{ ...external, enabled: false }, false],
        [{ ...external, listening: [] }, false],
        [{ ...external, problem: 'port busy' }, false],
        [{ ...external, externalHost: 'auto' }, false],
        [{ ...external, externalHost: 'auto', externalDetected: '203.0.113.50' }, true],
      ];
      for (const [patch, expected] of cases) {
        const { access } = setup({ getRemoteSettings: vi.fn(() => Promise.resolve(settingsOf(patch))) });
        await access.load();
        expect(access.canShareAddress.value, JSON.stringify(patch)).toBe(expected);
      }
    });

    it('asks the core for the payload on demand, and hides it', async () => {
      const { access, api } = setup({ getRemoteSettings: vi.fn(() => Promise.resolve(settingsOf(external))) });
      await access.load();
      expect(access.address.value).toBeNull();
      await access.showAddress();
      expect(api.getRemoteAddress).toHaveBeenCalledTimes(1);
      expect(access.address.value?.payload.startsWith('pouple-address:v1?ext=')).toBe(true);
      expect(access.addressLoading.value).toBe(false);
      access.hideAddress();
      expect(access.address.value).toBeNull();
    });

    it('keeps the core message when it refuses, and does not send twice at once', async () => {
      let release: () => void = () => undefined;
      const getRemoteAddress = vi.fn(() => new Promise<{ payload: string }>((_, reject) => (release = () => reject(new ApiError(409, 'No address is known yet.')))));
      const { access } = setup({ getRemoteAddress });
      await access.load();
      const first = access.showAddress();
      await access.showAddress();
      expect(getRemoteAddress).toHaveBeenCalledTimes(1);
      release();
      await first;
      expect(access.addressError.value).toBe('No address is known yet.');
      expect(access.address.value).toBeNull();
    });

    it('is dropped when a setting changes, because what it says may have changed', async () => {
      const { access } = setup({ getRemoteSettings: vi.fn(() => Promise.resolve(settingsOf(external))) });
      await access.load();
      await access.showAddress();
      await access.save({ externalPort: 8443 });
      expect(access.address.value).toBeNull();
    });

    it('ignores an answer that arrives after it was hidden', async () => {
      let resolve: (value: { payload: string }) => void = () => undefined;
      const getRemoteAddress = vi.fn(() => new Promise<{ payload: string }>((done) => (resolve = done)));
      const { access } = setup({ getRemoteAddress });
      await access.load();
      const pending = access.showAddress();
      access.hideAddress();
      resolve({ payload: 'pouple-address:v1?ext=late' });
      await pending;
      expect(access.address.value).toBeNull();
    });
  });

  it('saves the fields of the access from outside as one patch', async () => {
    const { access, api } = setup();
    await access.load();
    expect(await access.save({ externalEnabled: true, externalHost: 'home.example.net', externalPort: 8443 })).toBe(true);
    expect(api.updateRemoteSettings).toHaveBeenCalledWith({ externalEnabled: true, externalHost: 'home.example.net', externalPort: 8443 });
    expect(access.settings.value).toMatchObject({ externalEnabled: true, externalHost: 'home.example.net', externalPort: 8443 });
  });
});
