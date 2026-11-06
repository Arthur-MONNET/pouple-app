import { t } from '../i18n';
import type { ActionInfo, ConnectorStatus, Profile, ValidationFailure } from '../api/types';

/**
 * Where one connector's setup is. Nothing is saved before the user confirms who the connection belongs to:
 *   form -> checking -> confirm -> saving -> saved
 * and any failure goes back to the form with its message.
 */
export type FlowState =
  | { phase: 'form'; error: ValidationFailure | null }
  | { phase: 'checking' }
  | { phase: 'confirm'; profile: Profile }
  | { phase: 'saving'; profile: Profile }
  | { phase: 'saved'; profile: Profile };

export type FlowEvent =
  | { type: 'submit' }
  | { type: 'validated'; profile: Profile }
  | { type: 'failed'; error: ValidationFailure }
  | { type: 'reject' }
  | { type: 'confirm' }
  | { type: 'saved' };

export const initialFlow: FlowState = { phase: 'form', error: null };

/** Pure transition function; an event that makes no sense in the current phase leaves the state alone. */
export function flowReducer(state: FlowState, event: FlowEvent): FlowState {
  switch (event.type) {
    case 'submit':
      return state.phase === 'form' ? { phase: 'checking' } : state;
    case 'validated':
      return state.phase === 'checking' ? { phase: 'confirm', profile: event.profile } : state;
    case 'failed':
      return state.phase === 'checking' || state.phase === 'saving' ? { phase: 'form', error: event.error } : state;
    case 'reject':
      return state.phase === 'confirm' ? { phase: 'form', error: null } : state;
    case 'confirm':
      return state.phase === 'confirm' ? { phase: 'saving', profile: state.profile } : state;
    case 'saved':
      return state.phase === 'saving' ? { phase: 'saved', profile: state.profile } : state;
  }
}

/** A step with no field has nothing to type: it is checked on its own, then driven by its actions. */
export const hasForm = (connector: Pick<ConnectorStatus, 'descriptor'>): boolean => (connector.descriptor.fields ?? []).length > 0;

export interface ChecklistItem {
  id: string;
  label: string;
  ok: boolean;
  /** The action that fixes this item. */
  actionId: string | null;
}

/**
 * The checklist of a connector driven by actions, read from the message of its failed check. Keyed by connector id
 * because the core only says what is missing in words; with no error everything is in place.
 */
export function checklistFor(connectorId: string, actions: readonly ActionInfo[], error: ValidationFailure | null): ChecklistItem[] {
  if (connectorId !== 'codex' && connectorId !== 'claude') return [];
  const has = (id: string): string | null => (actions.some((action) => action.id === id) ? id : null);
  const notInstalled = error !== null && /not installed/i.test(error.message);
  const notSignedIn = error !== null && !notInstalled;
  return [
    { id: 'installed', label: t('checklist.installed'), ok: !notInstalled, actionId: has('install') },
    { id: 'signed-in', label: t('checklist.signedIn'), ok: !notInstalled && !notSignedIn, actionId: has('login') },
  ];
}

/**
 * The steps of the setup, in order: every required connector, and one connector for each group of alternatives
 * (Codex or Claude). In a group the one that is connected wins, then the one the reader picked, then the first.
 */
export function requiredSteps(connectors: readonly ConnectorStatus[], picked: Readonly<Record<string, string>> = {}): ConnectorStatus[] {
  const byOrder = [...connectors].sort((a, b) => a.descriptor.order - b.descriptor.order);
  const groups = new Set(byOrder.flatMap((c) => (c.descriptor.choice ? [c.descriptor.choice] : [])));
  const representative = (group: string): ConnectorStatus | undefined => {
    const members = byOrder.filter((c) => c.descriptor.choice === group);
    return members.find((c) => c.connected) ?? members.find((c) => c.descriptor.id === picked[group]) ?? members[0];
  };
  const chosen = new Set([...groups].flatMap((group) => representative(group)?.descriptor.id ?? []));
  return byOrder.filter((c) => (c.descriptor.choice ? chosen.has(c.descriptor.id) : c.descriptor.required));
}

/** The other members of the current step's group, so the reader can pick another AI. */
export const alternativesTo = (connectors: readonly ConnectorStatus[], step: ConnectorStatus): ConnectorStatus[] =>
  step.descriptor.choice && !step.connected
    ? connectors.filter((c) => c.descriptor.choice === step.descriptor.choice && c.descriptor.id !== step.descriptor.id && !c.connected)
    : [];

export type WizardPosition = { kind: 'welcome' } | { kind: 'step'; index: number } | { kind: 'projects' } | { kind: 'extras' } | { kind: 'done' };

/** Right after the last required connector is saved comes the optional projects folder, then the final screen. */
export const afterSave = (next: WizardPosition): WizardPosition => (next.kind === 'done' ? { kind: 'projects' } : next.kind === 'welcome' ? { kind: 'step', index: 0 } : next);

/** Resuming lands on the first required step that is not connected; the welcome screen only greets a fresh start. */
export function resumePosition(steps: readonly Pick<ConnectorStatus, 'connected'>[]): WizardPosition {
  const index = steps.findIndex((step) => !step.connected);
  if (index === -1) return { kind: 'done' };
  return index === 0 ? { kind: 'welcome' } : { kind: 'step', index };
}
