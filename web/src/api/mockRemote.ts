import { ApiError, type PoupleApi } from './client';
import type { RemoteDevice, RemoteInterface, RemoteSettings } from './types';

/** The part of the API that manages the phone access, in memory. */
export type MockRemote = Pick<
  PoupleApi,
  'getRemoteSettings' | 'updateRemoteSettings' | 'openPairing' | 'cancelPairing' | 'listRemoteDevices' | 'revokeRemoteDevice'
>;

const WINDOW_MS = 2 * 60_000;
const DEFAULT_PORT = 47821;

const INTERFACES: RemoteInterface[] = [
  { name: 'en0', addresses: ['192.168.1.24', 'fe80::1c2a:3bff:fe4d:5e6f'], loopback: false },
  { name: 'bridge100', addresses: ['192.168.64.1'], loopback: false },
  { name: 'lo0', addresses: ['127.0.0.1', '::1'], loopback: true },
];

interface MockRemoteOptions {
  /** Artificial latency, as the rest of the mock. */
  wait: () => Promise<void>;
  /** After this delay (ms) a phone "pairs" through an open window; `null` never (tests drive it themselves). */
  pairAfterMs: number | null;
}

/** The mock of the phone access: it follows the core's rules (an interface is needed, 1024-65535, a window of 2 minutes). */
export function createMockRemote({ wait, pairAfterMs }: MockRemoteOptions): MockRemote {
  let enabled = false;
  let interfaces: string[] = [];
  let port = 0;
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

  const view = (): RemoteSettings => ({
    enabled,
    interfaces: [...interfaces],
    port: port === 0 ? DEFAULT_PORT : port,
    available: structuredClone(INTERFACES),
    listening: enabled ? interfaces.flatMap((name) => (INTERFACES.find((item) => item.name === name)?.addresses ?? []).map((address) => `${address.includes(':') ? `[${address}]` : address}:${port === 0 ? DEFAULT_PORT : port}`)) : [],
    problem: '',
    fingerprint: enabled ? '9f2c4b1a0d3e5f6791a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f708' : '',
  });

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
      if (nextEnabled && nextInterfaces.length === 0) throw new ApiError(422, 'Choose the network the phone uses: Pouple never listens on every network at once.');
      enabled = nextEnabled;
      interfaces = [...nextInterfaces];
      port = patch.port ?? port;
      // The listener restarts: an open window does not survive it.
      close();
      return view();
    },
    async openPairing() {
      await wait();
      if (!enabled) throw new ApiError(409, 'Remote access is off.');
      windowEndsAt = Date.now() + WINDOW_MS;
      const code = Array.from({ length: 32 }, () => Math.floor(Math.random() * 16).toString(16)).join('');
      const host = view().listening[0]?.replace(/:\d+$/, '') ?? '127.0.0.1';
      const payload = `pouple-pair:v1?hosts=${encodeURIComponent(host)}%2CMy-Mac.local&port=${port === 0 ? DEFAULT_PORT : port}&fp=${view().fingerprint}&code=${code}&name=My-Mac`;
      clearTimeout(pairTimer);
      if (pairAfterMs !== null) {
        pairTimer = setTimeout(() => {
          if (Date.now() >= windowEndsAt) return;
          const now = new Date().toISOString();
          devices = [{ id: `p${Date.now().toString(16).padStart(31, '0')}`, name: 'Pixel 9 Pro', createdAt: now, lastSeen: now, revoked: false, expired: false }, ...devices];
          close();
        }, pairAfterMs);
      }
      return { payload, expiresAt: new Date(windowEndsAt).toISOString() };
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
