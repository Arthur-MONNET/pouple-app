import { describe, expect, it } from 'vitest';
import type { ConnectorStatus, Profile } from '../api/types';
import { afterSave, checklistFor, flowReducer, initialFlow, requiredSteps, resumePosition, type FlowState } from './connectorFlow';

const profile: Profile = { name: 'Arthur', facts: [], warnings: [] };
const status = (id: string, order: number, required: boolean, connected: boolean): ConnectorStatus => ({
  connected,
  descriptor: { id, name: id, tagline: '', required, order, fields: [], guide: [] },
});

describe('flowReducer', () => {
  it('walks form, checking, confirm, saving, saved', () => {
    let state: FlowState = initialFlow;
    state = flowReducer(state, { type: 'submit' });
    expect(state.phase).toBe('checking');
    state = flowReducer(state, { type: 'validated', profile });
    expect(state).toEqual({ phase: 'confirm', profile });
    state = flowReducer(state, { type: 'confirm' });
    expect(state.phase).toBe('saving');
    state = flowReducer(state, { type: 'saved' });
    expect(state).toEqual({ phase: 'saved', profile });
  });

  it('goes back to the form with the error when the check fails', () => {
    const state = flowReducer({ phase: 'checking' }, { type: 'failed', error: { field: 'token', message: 'nope' } });
    expect(state).toEqual({ phase: 'form', error: { field: 'token', message: 'nope' } });
  });

  it('goes back to the form when saving fails', () => {
    const state = flowReducer({ phase: 'saving', profile }, { type: 'failed', error: { message: 'keychain locked' } });
    expect(state).toEqual({ phase: 'form', error: { message: 'keychain locked' } });
  });

  it('"Not me" returns to a clean form without saving', () => {
    expect(flowReducer({ phase: 'confirm', profile }, { type: 'reject' })).toEqual({ phase: 'form', error: null });
  });

  it('cannot confirm or save from the wrong phase', () => {
    expect(flowReducer(initialFlow, { type: 'confirm' })).toBe(initialFlow);
    expect(flowReducer({ phase: 'checking' }, { type: 'saved' })).toEqual({ phase: 'checking' });
    expect(flowReducer({ phase: 'confirm', profile }, { type: 'submit' })).toEqual({ phase: 'confirm', profile });
  });
});

describe('checklistFor', () => {
  const actions = [{ id: 'install', label: 'Install' }, { id: 'login', label: 'Sign in' }];

  it('says what is missing from the check message', () => {
    const missing = checklistFor('codex', actions, { message: 'The Codex CLI is not installed yet. Click “Install Codex”.' });
    expect(missing.map((i) => i.ok)).toEqual([false, false]);
    const signedOut = checklistFor('codex', actions, { message: 'Not signed in yet. Click “Sign in with ChatGPT”.' });
    expect(signedOut.map((i) => i.ok)).toEqual([true, false]);
  });

  it('is all green with no error, and ties each item to its action', () => {
    const ok = checklistFor('codex', actions, null);
    expect(ok.every((i) => i.ok)).toBe(true);
    expect(ok.map((i) => i.actionId)).toEqual(['install', 'login']);
  });

  it('has nothing to say for connectors without a checklist', () => {
    expect(checklistFor('slack', [], null)).toEqual([]);
  });
});

describe('wizard position', () => {
  it('keeps the required steps in setup order and drops optional ones', () => {
    const steps = requiredSteps([status('codex', 2, true, false), status('asana', 3, false, false), status('slack', 1, true, false)]);
    expect(steps.map((s) => s.descriptor.id)).toEqual(['slack', 'codex']);
  });

  it('greets a fresh start with the welcome screen', () => {
    expect(resumePosition([status('slack', 1, true, false), status('codex', 2, true, false)])).toEqual({ kind: 'welcome' });
  });

  it('resumes on the first step that is not connected', () => {
    expect(resumePosition([status('slack', 1, true, true), status('codex', 2, true, false)])).toEqual({ kind: 'step', index: 1 });
  });

  it('ends on the final screen when everything is connected', () => {
    expect(resumePosition([status('slack', 1, true, true), status('codex', 2, true, true)])).toEqual({ kind: 'done' });
  });
});

describe('afterSave', () => {
  it('offers the projects folder once the last connector is saved', () => {
    expect(afterSave({ kind: 'done' })).toEqual({ kind: 'projects' });
    expect(afterSave({ kind: 'step', index: 1 })).toEqual({ kind: 'step', index: 1 });
    expect(afterSave({ kind: 'welcome' })).toEqual({ kind: 'step', index: 0 });
  });
});
