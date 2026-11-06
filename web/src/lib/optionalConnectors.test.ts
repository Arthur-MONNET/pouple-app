import { describe, expect, it } from 'vitest';
import type { ConnectorStatus } from '../api/types';
import { setLanguage, t } from '../i18n';
import {
  afterExtras,
  afterProjects,
  benefitKey,
  extraCards,
  extrasReducer,
  hasExtrasStep,
  HINT_KEY,
  initialExtras,
  optionalConnectors,
  readHintDismissed,
  shouldShowHint,
  writeHintDismissed,
  type HintStorage,
} from './optionalConnectors';

const status = (id: string, order: number, required: boolean, connected = false, profileName?: string): ConnectorStatus => ({
  connected,
  ...(profileName && { profile: { name: profileName, facts: null, warnings: null, avatarUrl: 'https://example.test/a.png' } }),
  descriptor: { id, name: id.toUpperCase(), tagline: `about ${id}`, required, order, fields: [], guide: [] },
});

const base = [status('slack', 1, true, true), status('codex', 2, true, true)];
const withExtras = [...base, status('slite', 12, false), status('gitlab', 10, false, true, 'Arthur'), status('asana', 11, false)];

describe('optional step', () => {
  it('exists only when the core offers an optional connector', () => {
    expect(hasExtrasStep(base)).toBe(false);
    expect(hasExtrasStep(withExtras)).toBe(true);
  });

  it('follows the projects folder when there are optional connectors, otherwise it is skipped', () => {
    expect(afterProjects(withExtras)).toEqual({ kind: 'extras' });
    expect(afterProjects(base)).toEqual({ kind: 'done' });
  });

  it('never blocks: continuing or skipping leads to the final screen, even with nothing connected', () => {
    expect(afterExtras()).toEqual({ kind: 'done' });
    expect(afterProjects(withExtras.map((c) => ({ ...c, connected: c.descriptor.required })))).toEqual({ kind: 'extras' });
  });

  it('opens one connector at a time, and closes when closed or saved', () => {
    let state = extrasReducer(initialExtras, { type: 'open', id: 'gitlab' });
    expect(state).toEqual({ openId: 'gitlab' });
    state = extrasReducer(state, { type: 'open', id: 'asana' });
    expect(state.openId).toBe('asana');
    expect(extrasReducer(state, { type: 'saved' })).toEqual(initialExtras);
    expect(extrasReducer(state, { type: 'close' })).toEqual(initialExtras);
    expect(extrasReducer(initialExtras, { type: 'close' })).toBe(initialExtras);
  });
});

describe('cards', () => {
  it('lists only optional connectors, in the core order', () => {
    expect(optionalConnectors(withExtras).map((c) => c.descriptor.id)).toEqual(['gitlab', 'asana', 'slite']);
  });

  it('shows who a connected one belongs to and nothing for the others', () => {
    const [gitlab, asana] = extraCards(withExtras);
    expect(gitlab).toMatchObject({ id: 'gitlab', connected: true, profileName: 'Arthur', avatarUrl: 'https://example.test/a.png' });
    expect(asana).toMatchObject({ id: 'asana', connected: false, profileName: null, avatarUrl: null });
  });

  it('uses a known line per known id and the generic one for an id it has never heard of', () => {
    setLanguage('en');
    for (const id of ['gitlab', 'asana', 'slite']) {
      expect(benefitKey(id)).toBe(`extras.benefit.${id}`);
      expect(t(benefitKey(id))).not.toBe(t('extras.benefit.generic'));
    }
    const [unknown] = extraCards([status('notion', 20, false)]);
    expect(unknown?.benefitKey).toBe('extras.benefit.generic');
    expect(t(unknown?.benefitKey ?? '')).toBe(t('extras.benefit.generic'));
  });
});

describe('hint', () => {
  it('shows once setup is complete and no optional connector is connected', () => {
    const none = withExtras.map((c) => ({ ...c, connected: c.descriptor.required }));
    expect(shouldShowHint(none, true, false)).toBe(true);
  });

  it('stays hidden before setup is complete, once dismissed, when one is connected, or when there is nothing to offer', () => {
    const none = withExtras.map((c) => ({ ...c, connected: c.descriptor.required }));
    expect(shouldShowHint(none, false, false)).toBe(false);
    expect(shouldShowHint(none, true, true)).toBe(false);
    expect(shouldShowHint(withExtras, true, false)).toBe(false);
    expect(shouldShowHint(base, true, false)).toBe(false);
  });

  const memory = (): HintStorage & { data: Map<string, string> } => {
    const data = new Map<string, string>();
    return { data, getItem: (key) => data.get(key) ?? null, setItem: (key, value) => void data.set(key, value) };
  };
  const broken: HintStorage = {
    getItem: () => {
      throw new Error('blocked');
    },
    setItem: () => {
      throw new Error('blocked');
    },
  };

  it('remembers the dismissal', () => {
    const storage = memory();
    expect(readHintDismissed(storage)).toBe(false);
    writeHintDismissed(storage);
    expect(storage.data.get(HINT_KEY)).toBe('1');
    expect(readHintDismissed(storage)).toBe(true);
  });

  it('survives storage that is missing or throws', () => {
    expect(readHintDismissed(broken)).toBe(false);
    expect(() => writeHintDismissed(broken)).not.toThrow();
    expect(readHintDismissed(null)).toBe(false);
    expect(() => writeHintDismissed(null)).not.toThrow();
  });
});
