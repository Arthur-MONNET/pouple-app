import { describe, expect, it } from 'vitest';
import { defaultOpenKeys, FOLDS_KEY, isTopicOpen, pruneFolds, readFolds, shownRequests, writeFolds, type FoldStorage } from './topicFolds';
import type { ConversationGroup } from './topicGroups';

const group = (key: string, ids: string[], attention = 0): ConversationGroup => ({
  key, label: key, attention, requests: ids.map((id) => ({ id }) as ConversationGroup['requests'][number]),
});

function memory(initial?: string): FoldStorage & { value: string | null } {
  const box = { value: initial ?? null, getItem: () => box.value, setItem: (_key: string, value: string) => void (box.value = value) };
  return box;
}

describe('defaultOpenKeys', () => {
  it('folds everything when nothing is selected and nothing needs attention', () => {
    expect(defaultOpenKeys([group('a', ['1']), group('b', ['2'])], null).size).toBe(0);
  });

  it('opens the topic of the selected conversation', () => {
    expect([...defaultOpenKeys([group('a', ['1']), group('b', ['2', '3'])], '3')]).toEqual(['b']);
  });

  it('opens at most the 3 most recent topics needing attention', () => {
    const groups = [group('a', ['1'], 1), group('b', ['2']), group('c', ['3'], 2), group('d', ['4'], 1), group('e', ['5'], 1)];
    expect([...defaultOpenKeys(groups, null)].sort()).toEqual(['a', 'c', 'd']);
  });

  it('adds the selected topic on top of the attention ones', () => {
    const groups = [group('a', ['1'], 1), group('b', ['2'], 1), group('c', ['3'], 1), group('d', ['4'], 1), group('e', ['5'])];
    expect(defaultOpenKeys(groups, '5').size).toBe(4);
  });
});

describe('isTopicOpen', () => {
  it('lets an explicit choice win over the default, both ways', () => {
    const defaults = new Set(['a']);
    expect(isTopicOpen('a', {}, defaults)).toBe(true);
    expect(isTopicOpen('a', { a: false }, defaults)).toBe(false);
    expect(isTopicOpen('b', { b: true }, defaults)).toBe(true);
    expect(isTopicOpen('b', {}, defaults)).toBe(false);
  });
});

describe('persistence', () => {
  it('writes and reads the overrides back', () => {
    const storage = memory();
    writeFolds(storage, { a: true, b: false });
    expect(storage.value).not.toBeNull();
    expect(readFolds(storage)).toEqual({ a: true, b: false });
  });

  it('reads nothing from garbage or from the wrong shape', () => {
    expect(readFolds(memory('{nope'))).toEqual({});
    expect(readFolds(memory('[1]'))).toEqual({});
    expect(readFolds(memory('null'))).toEqual({});
    expect(readFolds(memory(JSON.stringify({ a: true, b: 'x', c: 1 })))).toEqual({ a: true });
  });

  it('survives a missing or throwing storage', () => {
    const broken: FoldStorage = { getItem: () => { throw new Error('denied'); }, setItem: () => { throw new Error('full'); } };
    expect(readFolds(null)).toEqual({});
    expect(readFolds(broken)).toEqual({});
    expect(() => writeFolds(null, { a: true })).not.toThrow();
    expect(() => writeFolds(broken, { a: true })).not.toThrow();
  });

  it('uses its own key', () => {
    expect(FOLDS_KEY).toBe('pouple.topicFolds');
  });
});

describe('pruneFolds', () => {
  it('drops the topics that no longer exist and keeps the object when nothing goes', () => {
    const folds = { a: true, gone: false };
    expect(pruneFolds(folds, new Set(['a']))).toEqual({ a: true });
    const same = { a: true };
    expect(pruneFolds(same, new Set(['a', 'b']))).toBe(same);
  });
});

describe('shownRequests', () => {
  const ids = (list: { id: string }[]): string[] => list.map((item) => item.id);
  const many = Array.from({ length: 8 }, (_, index) => ({ id: `r${index}` }));

  it('shows the first 5 of an open topic and counts the rest', () => {
    const { shown, hidden } = shownRequests(many, true, false, null);
    expect(ids(shown)).toEqual(['r0', 'r1', 'r2', 'r3', 'r4']);
    expect(hidden).toBe(3);
  });

  it('shows everything once expanded', () => {
    expect(shownRequests(many, true, true, null)).toMatchObject({ hidden: 0, shown: { length: 8 } });
  });

  it('shows only the selected conversation of a folded topic', () => {
    expect(shownRequests(many, false, false, 'r6')).toEqual({ shown: [{ id: 'r6' }], hidden: 0 });
    expect(shownRequests(many, false, false, null)).toEqual({ shown: [], hidden: 0 });
  });

  it('keeps a selected conversation that sits past the first 5', () => {
    const { shown, hidden } = shownRequests(many, true, false, 'r6');
    expect(ids(shown)).toEqual(['r0', 'r1', 'r2', 'r3', 'r4', 'r6']);
    expect(hidden).toBe(2);
  });
});
