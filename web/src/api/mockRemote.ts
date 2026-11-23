import { ApiError, type PoupleApi } from './client';
import { checkExternalHost, checkTunnelDomain, EXTERNAL_AUTO, externalEndpoint, localAddress } from '../lib/phonePairing';
import type { RemoteDevice, RemoteInterface, RemoteSettings, TunnelStatus } from './types';

/** The part of the API that manages the phone access, in memory. */
export type MockRemote = Pick<
  PoupleApi,
  'getRemoteSettings' | 'updateRemoteSettings' | 'getRemoteAddress' | 'openPairing' | 'cancelPairing' | 'listRemoteDevices' | 'revokeRemoteDevice'
>;

const WINDOW_MS = 2 * 60_000;
const DEFAULT_PORT = 47821;
/** What the mock "detects" for an `auto` host. */
const DETECTED_ADDRESS = '203.0.113.50';
/** How long the mock tunnel takes to go from `starting` to `running`. */
const TUNNEL_START_MS = 1500;
/** The token that makes the mock tunnel fail, to show the error state. */
export const MOCK_BAD_TOKEN = 'invalid';
const FINGERPRINT = '9f2c4b1a0d3e5f6791a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f708';

const INTERFACES: RemoteInterface[] = [
  { name: 'en0', addresses: ['192.168.1.24', 'fe80::1c2a:3bff:fe4d:5e6f'], loopback: false },
  { name: 'bridge100', addresses: ['192.168.64.1'], loopback: false },
  { name: 'lo0', addresses: ['127.0.0.1', '::1'], loopback: true },
];

/** RFC 1918 IPv4 and ULA IPv6 addresses: what the automatic choice of networks listens on. */
const isPrivate = (address: string): boolean => /^(10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.|f[cd][0-9a-f]{2}:)/i.test(address);

interface MockRemoteOptions {
  /** Artificial latency, as the rest of the mock. */
  wait: () => Promise<void>;
  /** After this delay (ms) a phone "pairs" through an open window; `null` never (tests drive it themselves). */
  pairAfterMs: number | null;
}

/** The mock of the phone access: it follows the core's rules (no interface chosen means automatic, 1024-65535, a window of 2 minutes). */
export function createMockRemote({ wait, pairAfterMs }: MockRemoteOptions): MockRemote {
  let enabled = false;
  let interfaces: string[] = [];
  let port = 0;
  let externalEnabled = false;
  let externalHost = '';
  let externalPort = 0;
  let detectedAt = '';
  let tunnelEnabled = false;
  let tunnelDomain = '';
  let tunnelToken = '';
  let tunnelStartedAt = 0;
  let windowEndsAt = 0;
  let pairTimer: ReturnType<typeof setTimeout> | undefined;
  let devices: RemoteDevice[] = [
    {
      id: 'd1a2b3c4d5e6f708192a3b4c5d6e7f80',
      name: 'Pixel 8',
      createdAt: new Date(Date.now() - 9 * 86400_000).toISOString(),
      lastSeen: new Date(Date.now() - 12 * 60_000).toISOString(),
      revoked: false,
      expired: false,
    },
    {
      id: '0f1e2d3c4b5a69788796a5b4c3d2e1f0',
      name: 'Galaxy S21',
      createdAt: new Date(Date.now() - 60 * 86400_000).toISOString(),
      lastSeen: new Date(Date.now() - 41 * 86400_000).toISOString(),
      revoked: true,
      expired: true,
    },
  ];

  /** The core detects the public address only for an `auto` host while both accesses are on. */
  const detects = (): boolean => enabled && externalEnabled && externalHost === EXTERNAL_AUTO;

  const tunnelStatus = (): TunnelStatus => {
    if (!tunnelEnabled) return 'off';
    if (tunnelToken === MOCK_BAD_TOKEN) return 'error';
    return Date.now() - tunnelStartedAt < TUNNEL_START_MS ? 'starting' : 'running';
  };

  /** The addresses the listener binds: the chosen interfaces, or with none chosen every private address that is up. */
  const bound = (): string[] =>
    interfaces.length > 0
      ? interfaces.flatMap((name) => INTERFACES.find((item) => item.name === name)?.addresses ?? [])
      : INTERFACES.filter((item) => !item.loopback).flatMap((item) => item.addresses.filter(isPrivate));

  const view = (): RemoteSettings => {
    const listening = enabled ? bound().map((address) => `${address.includes(':') ? `[${address}]` : address}:${port === 0 ? DEFAULT_PORT : port}`) : [];
    const next = {
      enabled,
      interfaces: [...interfaces],
      port: port === 0 ? DEFAULT_PORT : port,
      available: structuredClone(INTERFACES),
      listening,
      problem: '',
      fingerprint: enabled ? FINGERPRINT : '',
      externalEnabled,
      externalHost,
      externalPort,
      externalDetected: detects() ? DETECTED_ADDRESS : '',
      externalDetectedAt: detects() ? detectedAt : '',
    };
    const status = tunnelStatus();
    return {
      ...next,
      externalReady: enabled && externalEndpoint(next) !== null && localAddress(listening) !== null,
      tunnelEnabled,
      tunnelDomain,
      tunnelAuthtokenSet: tunnelToken !== '',
      tunnelStatus: status,
      tunnelProblem: status === 'error' ? 'ngrok refused the authtoken: copy it again from the Your Authtoken page of the ngrok dashboard.' : '',
    };
  };

  const close = (): void => {
    windowEndsAt = 0;
    clearTimeout(pairTimer);
  };

  return {
    async getRemoteSettings() {
      await wait();
      return view();
    },
    async updateRemoteSettings(patch) {
      await wait();
      const nextInterfaces = patch.interfaces ?? interfaces;
      const unknown = nextInterfaces.find((name) => !INTERFACES.some((item) => item.name === name));
      if (unknown) throw new ApiError(422, `“${unknown}” is not the name of a network interface.`);
      if (patch.port !== undefined && patch.port !== 0 && (patch.port < 1024 || patch.port > 65535)) throw new ApiError(422, 'The port must be between 1024 and 65535.');
      const nextEnabled = patch.enabled ?? enabled;
      let nextHost = externalHost;
      if (patch.externalHost !== undefined) {
        const checked = checkExternalHost(patch.externalHost);
        if (!checked.ok && checked.reason === 'invalid') throw new ApiError(422, `“${patch.externalHost.slice(0, 64)}” is not a valid public name or address: use a host name such as home.example.net, or an IP address, with no http://, port, path or space.`);
        nextHost = checked.ok ? checked.host : '';
      }
      if (patch.externalPort !== undefined && (!Number.isInteger(patch.externalPort) || patch.externalPort < 0 || patch.externalPort > 65535)) throw new ApiError(422, 'The external port must be 0 (the same as the port above) or between 1 and 65535.');
      const nextExternal = patch.externalEnabled ?? externalEnabled;
      if (nextExternal && nextHost === '') throw new ApiError(422, 'Enter the public name or address of your home network to reach the Mac from outside.');
      let nextDomain = tunnelDomain;
      if (patch.tunnelDomain !== undefined) {
        const checked = checkTunnelDomain(patch.tunnelDomain);
        if (!checked.ok && checked.reason === 'invalid') throw new ApiError(422, `“${patch.tunnelDomain.slice(0, 64)}” is not a valid domain: use the static domain of your ngrok account, such as name.ngrok-free.app, with no https://, port or path.`);
        nextDomain = checked.ok ? checked.domain : '';
      }
      const nextToken = patch.tunnelAuthtoken !== undefined && patch.tunnelAuthtoken !== '' ? patch.tunnelAuthtoken : tunnelToken;
      const nextTunnel = patch.tunnelEnabled ?? tunnelEnabled;
      if (nextTunnel && nextDomain === '') throw new ApiError(422, 'Enter your static ngrok domain to turn the tunnel on.');
      if (nextTunnel && nextToken === '') throw new ApiError(422, 'Enter your ngrok authtoken to turn the tunnel on.');
      if (nextTunnel && (!tunnelEnabled || nextDomain !== tunnelDomain || nextToken !== tunnelToken)) tunnelStartedAt = Date.now();
      enabled = nextEnabled;
      tunnelEnabled = nextTunnel;
      tunnelDomain = nextDomain;
      tunnelToken = nextToken;
      interfaces = [...nextInterfaces];
      port = patch.port ?? port;
      externalEnabled = nextExternal;
      externalHost = nextHost;
      externalPort = patch.externalPort ?? externalPort;
      detectedAt = new Date().toISOString();
      // The listener restarts: an open window does not survive it.
      close();
      return view();
    },
    async getRemoteAddress() {
      await wait();
      const next = view();
      if (!enabled) throw new ApiError(409, 'Turn on access from the phone, on a network that is connected, before pairing one.');
      const ext = externalEndpoint(next);
      if (ext === null) throw new ApiError(409, 'Turn on access from outside and enter an address, or wait for Pouple to detect it, before sharing it with the phone.');
      return { payload: `pouple-address:v1?ext=${encodeURIComponent(ext)}&fp=${next.fingerprint}` };
    },
    async openPairing() {
      await wait();
      if (!enabled) throw new ApiError(409, 'Remote access is off.');
      windowEndsAt = Date.now() + WINDOW_MS;
      const code = Array.from({ length: 32 }, () => Math.floor(Math.random() * 16).toString(16)).join('');
      const host = view().listening[0]?.replace(/:\d+$/, '') ?? '127.0.0.1';
      const payload = `pouple-pair:v1?hosts=${encodeURIComponent(host)}%2CMy-Mac.local&port=${port === 0 ? DEFAULT_PORT : port}&fp=${view().fingerprint}&code=${code}&name=My-Mac`;
      const ext = externalEndpoint(view());
      const withExt = ext === null ? payload : `${payload}&ext=${encodeURIComponent(ext)}`;
      const payloadWithExt = tunnelEnabled ? `${withExt}&tun=${encodeURIComponent(tunnelDomain)}` : withExt;
      clearTimeout(pairTimer);
      if (pairAfterMs !== null) {
        pairTimer = setTimeout(() => {
          if (Date.now() >= windowEndsAt) return;
          const now = new Date().toISOString();
          devices = [{ id: `p${Date.now().toString(16).padStart(31, '0')}`, name: 'Pixel 9 Pro', createdAt: now, lastSeen: now, revoked: false, expired: false }, ...devices];
          close();
        }, pairAfterMs);
      }
      return { payload: payloadWithExt, expiresAt: new Date(windowEndsAt).toISOString() };
    },
    async cancelPairing() {
      await wait();
      close();
    },
    async listRemoteDevices() {
      await wait();
      return structuredClone(devices);
    },
    async revokeRemoteDevice(id) {
      await wait();
      const device = devices.find((item) => item.id === id);
      if (!device) throw new ApiError(404, 'This device is not paired.');
      device.revoked = true;
    },
  };
}
