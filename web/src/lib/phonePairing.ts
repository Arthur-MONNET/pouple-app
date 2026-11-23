import type { RemoteDevice, RemoteInterface, RemoteSettings } from '../api/types';

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

/** The network a phone would use by default: the first interface that is not the loopback one. */
export function defaultInterfaces(available: readonly RemoteInterface[]): string[] {
  const first = available.find((item) => !item.loopback && item.addresses.length > 0);
  return first ? [first.name] : [];
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
