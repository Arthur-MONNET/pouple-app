import type { ConnectorStatus } from '../api/types';
import type { WizardPosition } from './connectorFlow';

/** Connectors the user may add after setup: the core marks them `required: false`. */
export const optionalConnectors = (connectors: readonly ConnectorStatus[]): ConnectorStatus[] =>
  connectors.filter((c) => !c.descriptor.required).sort((a, b) => a.descriptor.order - b.descriptor.order);

/** Ids that have their own "what it brings" line in the catalogs; any other id falls back to the generic one. */
const KNOWN_BENEFITS: ReadonlySet<string> = new Set(['gitlab', 'asana', 'slite']);

/** The i18n key of the line saying what a connector brings to the assistant. */
export const benefitKey = (id: string): string => (KNOWN_BENEFITS.has(id) ? `extras.benefit.${id}` : 'extras.benefit.generic');

/** One card of the optional step. */
export interface ExtraCard {
  id: string;
  name: string;
  tagline: string;
  benefitKey: string;
  connected: boolean;
  /** Who the connection belongs to, once connected. */
  profileName: string | null;
  avatarUrl: string | null;
}

/** Every optional connector, connected or not, in the core's order. */
export const extraCards = (connectors: readonly ConnectorStatus[]): ExtraCard[] =>
  optionalConnectors(connectors).map((c) => ({
    id: c.descriptor.id,
    name: c.descriptor.name,
    tagline: c.descriptor.tagline,
    benefitKey: benefitKey(c.descriptor.id),
    connected: c.connected,
    profileName: c.connected ? (c.profile?.name ?? null) : null,
    avatarUrl: c.connected ? (c.profile?.avatarUrl ?? null) : null,
  }));

/** The optional step only exists when the core offers at least one optional connector. */
export const hasExtrasStep = (connectors: readonly ConnectorStatus[]): boolean => optionalConnectors(connectors).length > 0;

/**
 * Where the wizard goes once the projects folder is settled (saved or skipped): the optional connectors when there
 * are some, otherwise straight to the final screen.
 */
export const afterProjects = (connectors: readonly ConnectorStatus[]): WizardPosition => (hasExtrasStep(connectors) ? { kind: 'extras' } : { kind: 'done' });

/** The optional step never blocks: continuing and skipping both lead to the final screen. */
export const afterExtras = (): WizardPosition => ({ kind: 'done' });

/** The side sheet of the optional step: at most one connector open at a time. */
export interface ExtrasState {
  openId: string | null;
}
export type ExtrasEvent = { type: 'open'; id: string } | { type: 'close' } | { type: 'saved' };
export const initialExtras: ExtrasState = { openId: null };

export function extrasReducer(state: ExtrasState, event: ExtrasEvent): ExtrasState {
  switch (event.type) {
    case 'open':
      return { openId: event.id };
    case 'close':
    case 'saved':
      return state.openId === null ? state : { openId: null };
  }
}

/**
 * The hint of the conversations view: only once setup is complete, only when there is something to offer, only while
 * none of it is connected, and never again after it was dismissed.
 */
export function shouldShowHint(connectors: readonly ConnectorStatus[], setupComplete: boolean, dismissed: boolean): boolean {
  if (!setupComplete || dismissed) return false;
  const optional = optionalConnectors(connectors);
  return optional.length > 0 && optional.every((c) => !c.connected);
}

export const HINT_KEY = 'pouple.extras-hint.dismissed';

/** The few methods of Storage this file needs, so tests can pass a fake. */
export type HintStorage = Pick<Storage, 'getItem' | 'setItem'>;

const browserStorage = (): HintStorage | null => {
  try {
    return globalThis.localStorage ?? null;
  } catch {
    return null;
  }
};

/** Whether the hint was dismissed. Storage can be missing or throw (private window): the hint then simply shows. */
export function readHintDismissed(storage: HintStorage | null = browserStorage()): boolean {
  try {
    return storage?.getItem(HINT_KEY) === '1';
  } catch {
    return false;
  }
}

export function writeHintDismissed(storage: HintStorage | null = browserStorage()): void {
  try {
    storage?.setItem(HINT_KEY, '1');
  } catch {
    // Not remembered: the hint comes back next time, which is harmless.
  }
}
