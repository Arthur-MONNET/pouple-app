import type { RemoteDevice, RemoteInterface, RemoteSettings, RemoteSettingsPatch } from '../api/types';

/** How long the core keeps a pairing window open (docs/REMOTE-API.md, Pairing). */
export const PAIRING_WINDOW_SECONDS = 120;

export const MIN_PORT = 1024;
export const MAX_PORT = 65535;
export const DEFAULT_PORT = 47821;

/** Whole seconds until the window closes, never below 0; a date that cannot be read counts as closed. */
export function secondsLeft(expiresAt: string, now: number): number {
  const end = new Date(expiresAt).getTime();
  if (Number.isNaN(end)) return 0;
  return Math.max(0, Math.ceil((end - now) / 1000));
}

/** "1:59", "0:05": a countdown a person can read at a glance. */
export function formatCountdown(seconds: number): string {
  const whole = Math.max(0, Math.floor(seconds));
  return `${Math.floor(whole / 60)}:${String(whole % 60).padStart(2, '0')}`;
}

/** The share of the window still open, from 0 to 1, for a progress bar. */
export function windowShare(seconds: number): number {
  return Math.min(1, Math.max(0, seconds / PAIRING_WINDOW_SECONDS));
}

export const isValidPort = (port: number): boolean => Number.isInteger(port) && port >= MIN_PORT && port <= MAX_PORT;

/** The devices that appeared since `known` was taken and that can still be used: a phone that just paired. */
export function freshDevices(known: ReadonlySet<string>, devices: readonly RemoteDevice[]): RemoteDevice[] {
  return devices.filter((device) => !known.has(device.id) && !device.revoked);
}

export type DeviceStatus = 'active' | 'revoked' | 'expired';

/** A revoked device stays revoked even when it also went unused for long. */
export function deviceStatus(device: Pick<RemoteDevice, 'revoked' | 'expired'>): DeviceStatus {
  if (device.revoked) return 'revoked';
  return device.expired ? 'expired' : 'active';
}

export interface InterfaceChoice extends RemoteInterface {
  /** Chosen in the settings but not offered by the Mac any more (unplugged, Wi-Fi off). */
  missing: boolean;
  selected: boolean;
}

/** The interfaces on offer, plus the chosen ones that went away, so a stale choice stays visible and can be unticked. */
export function interfaceChoices(settings: Pick<RemoteSettings, 'available' | 'interfaces'>): InterfaceChoice[] {
  const chosen = new Set(settings.interfaces);
  const offered = settings.available.map((item) => ({ ...item, missing: false, selected: chosen.has(item.name) }));
  const gone = settings.interfaces
    .filter((name) => !settings.available.some((item) => item.name === name))
    .map((name) => ({ name, addresses: [], loopback: false, missing: true, selected: true }));
  return [...offered, ...gone];
}

/** Toggles one interface in the chosen list, keeping the order of the choices. */
export function toggleInterface(chosen: readonly string[], name: string, on: boolean): string[] {
  const rest = chosen.filter((item) => item !== name);
  return on ? [...rest, name] : rest;
}

/** The external host that asks the core to find the public address itself. */
export const EXTERNAL_AUTO = 'auto';

/** The longest DNS name there is (RFC 1035). */
export const MAX_HOST_LENGTH = 253;

export type ExternalHostCheck = { ok: true; host: string } | { ok: false; reason: 'required' | 'invalid' };

const IPV4 = /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$/;
const DNS_LABEL = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;

/** An IPv4 address nobody can reach from outside: unspecified, loopback, link-local, multicast and above. */
function unusableIPv4(host: string): boolean {
  const [first = 0, second = 0] = host.split('.').map(Number);
  return first === 0 || first === 127 || first >= 224 || (first === 169 && second === 254);
}

/** The canonical form of a bare IPv6 literal, or null. The browser's own parser does the work. */
function canonicalIPv6(host: string): string | null {
  if (!/^[0-9a-fA-F:.]+$/.test(host)) return null;
  try {
    const parsed = new URL(`http://[${host}]/`).hostname;
    return parsed.startsWith('[') && parsed.endsWith(']') ? parsed.slice(1, -1) : null;
  } catch {
    return null;
  }
}

/**
 * Checks the public name or address the way the core does (core/internal/remote/external.go): trimmed, a DNS name, an
 * IPv4 address, a bare IPv6 address or `auto`; no scheme, path, port, space or zone; at most 253 characters. The core
 * stays the judge: this only tells the person early, and sends the same canonical form.
 */
export function checkExternalHost(text: string): ExternalHostCheck {
  const host = text.trim();
  if (host === '') return { ok: false, reason: 'required' };
  const invalid = { ok: false, reason: 'invalid' } as const;
  if (host.length > MAX_HOST_LENGTH) return invalid;
  const lower = host.toLowerCase();
  if (lower === EXTERNAL_AUTO) return { ok: true, host: EXTERNAL_AUTO };
  if (host.includes(':')) {
    const v6 = canonicalIPv6(host);
    if (v6 === null || v6.startsWith('::ffff:') || v6 === '::' || v6 === '::1' || /^fe[89ab]/.test(v6) || v6.startsWith('ff')) return invalid;
    return { ok: true, host: v6 };
  }
  if (IPV4.test(host)) return unusableIPv4(host) ? invalid : { ok: true, host };
  const labels = lower.split('.');
  if (!labels.every((label) => DNS_LABEL.test(label)) || /^\d+$/.test(labels[labels.length - 1] ?? '')) return invalid;
  return { ok: true, host: lower };
}

/** The port the router exposes: empty or 0 is "the same as the listening port", otherwise a TCP port. */
export function parseExternalPort(text: string): number | null {
  const trimmed = text.trim();
  if (trimmed === '') return 0;
  if (!/^\d{1,5}$/.test(trimmed)) return null;
  const port = Number(trimmed);
  return port <= 65535 ? port : null;
}

/** The TCP port to forward on the router: the external one, or the listening port when none was chosen. */
export const forwardedPort = (settings: Pick<RemoteSettings, 'port' | 'externalPort'>): number => settings.externalPort || settings.port;

/** The first address of the listener that is on a real network: what the router forwards to. Brackets are dropped. */
export function localAddress(listening: readonly string[]): string | null {
  for (const address of listening) {
    const host = address.replace(/:\d+$/, '').replace(/^\[|\]$/g, '');
    if (host !== '' && host !== '127.0.0.1' && host !== '::1' && !host.startsWith('127.')) return host;
  }
  return null;
}

/** The `ext` of the pairing payload as the core builds it: `host:port`, an IPv6 literal bracketed; null while unknown. */
export function externalEndpoint(settings: Pick<RemoteSettings, 'port' | 'externalEnabled' | 'externalHost' | 'externalPort' | 'externalDetected'>): string | null {
  if (!settings.externalEnabled) return null;
  const host = settings.externalHost === EXTERNAL_AUTO ? settings.externalDetected : settings.externalHost;
  if (host === '') return null;
  return `${host.includes(':') ? `[${host}]` : host}:${forwardedPort(settings)}`;
}

export type TunnelDomainCheck = { ok: true; domain: string } | { ok: false; reason: 'required' | 'invalid' };

/**
 * Checks the static ngrok domain the way the core does: trimmed, lower-cased, a strict DNS name with at least two
 * labels (`name.ngrok-free.app`), no scheme, port, path, space or IP address, at most 253 characters.
 */
export function checkTunnelDomain(text: string): TunnelDomainCheck {
  const domain = text.trim().toLowerCase();
  if (domain === '') return { ok: false, reason: 'required' };
  const labels = domain.split('.');
  const valid = domain.length <= MAX_HOST_LENGTH && labels.length >= 2 && labels.every((label) => DNS_LABEL.test(label)) && !/^\d+$/.test(labels[labels.length - 1] ?? '');
  return valid ? { ok: true, domain } : { ok: false, reason: 'invalid' };
}

export type TunnelPatchResult = { ok: true; patch: RemoteSettingsPatch } | { ok: false; reason: 'required' | 'invalid' | 'token' };

/**
 * The patch that saves the tunnel fields. The authtoken is write-only: it goes out only when the person typed one,
 * never as an empty string, so saving the domain never erases or resends the stored token. Turning the tunnel on
 * needs a valid domain and a token, stored already (`tokenStored`) or typed now.
 */
export function tunnelPatch(input: { domain: string; token: string; tokenStored: boolean; enable?: boolean }): TunnelPatchResult {
  const checked = checkTunnelDomain(input.domain);
  const token = input.token.trim();
  if (!checked.ok) {
    // An empty domain is only a problem when turning the tunnel on.
    if (checked.reason === 'required' && input.enable !== true) return { ok: true, patch: { tunnelDomain: '', ...(token === '' ? {} : { tunnelAuthtoken: token }) } };
    return checked;
  }
  if (input.enable === true && token === '' && !input.tokenStored) return { ok: false, reason: 'token' };
  const patch: RemoteSettingsPatch = { tunnelDomain: checked.domain };
  if (token !== '') patch.tunnelAuthtoken = token;
  if (input.enable !== undefined) patch.tunnelEnabled = input.enable;
  return { ok: true, patch };
}
