import { nextTick, ref } from 'vue';
import { describe, expect, it } from 'vitest';
import { FOLDS_KEY, type FoldStorage } from '../lib/topicFolds';
import type { ConversationGroup } from '../lib/topicGroups';
import { useTopicFolds } from './useTopicFolds';

const group = (key: string, ids: string[], attention = 0): ConversationGroup => ({
  key, label: key, attention, requests: ids.map((id) => ({ id }) as ConversationGroup['requests'][number]),
});

function memory(initial?: Record<string, boolean>): FoldStorage & { saved: () => unknown } {
  let value: string | null = initial ? JSON.stringify(initial) : null;
  return { getItem: () => value, setItem: (_key, next) => void (value = next), saved: () => (value ? JSON.parse(value) : null) };
}

describe('useTopicFolds', () => {
  it('follows the defaults until the user toggles', () => {
    const groups = ref([group('a', ['1'], 1), group('b', ['2'])]);
    const folds = useTopicFolds(groups, () => null, memory());
    expect([folds.isOpen('a'), folds.isOpen('b')]).toEqual([true, false]);
    folds.toggle('a');
    folds.toggle('b');
    expect([folds.isOpen('a'), folds.isOpen('b')]).toEqual([false, true]);
  });

  it('persists the choices and restores them', () => {
    const storage = memory();
    const groups = ref([group('a', ['1']), group('b', ['2'])]);
    useTopicFolds(groups, () => null, storage).toggle('a');
    expect(storage.saved()).toEqual({ a: true });
    expect(useTopicFolds(groups, () => null, storage).isOpen('a')).toBe(true);
  });

  it('does not force open a topic the user folded when the selection moves into it', () => {
    const selected = ref<string | null>(null);
    const groups = ref([group('a', ['1']), group('b', ['2'])]);
    const folds = useTopicFolds(groups, selected, memory({ b: false }));
    selected.value = '2';
    expect(folds.isOpen('b')).toBe(false);
    selected.value = '1';
    expect(folds.isOpen('a')).toBe(true);
  });

  it('opens or folds every given topic at once', () => {
    const groups = ref([group('a', ['1'], 1), group('b', ['2'])]);
    const folds = useTopicFolds(groups, () => null, memory());
    expect(folds.anyOpen(['a', 'b'])).toBe(true);
    folds.setAll(['a', 'b'], false);
    expect(folds.anyOpen(['a', 'b'])).toBe(false);
    folds.setAll(['a', 'b'], true);
    expect([folds.isOpen('a'), folds.isOpen('b')]).toEqual([true, true]);
  });

  it('prunes the topics that disappeared, but not while the list is empty', async () => {
    const storage = memory({ a: true, gone: false });
    const groups = ref<ConversationGroup[]>([]);
    useTopicFolds(groups, () => null, storage);
    expect(storage.saved()).toEqual({ a: true, gone: false });
    groups.value = [group('a', ['1'])];
    await nextTick();
    expect(storage.saved()).toEqual({ a: true });
  });

  it('works without any storage', () => {
    const broken: FoldStorage = { getItem: () => { throw new Error('denied'); }, setItem: () => { throw new Error('denied'); } };
    for (const storage of [null, broken]) {
      const folds = useTopicFolds([group('a', ['1'])], () => null, storage);
      expect(folds.isOpen('a')).toBe(false);
      folds.toggle('a');
      expect(folds.isOpen('a')).toBe(true);
    }
    expect(FOLDS_KEY).toBeTruthy();
  });
});
