import { computed, ref, toValue, watch, type MaybeRefOrGetter } from 'vue';
import {
  browserStorage,
  defaultOpenKeys,
  isTopicOpen,
  pruneFolds,
  readFolds,
  writeFolds,
  type FoldOverrides,
  type FoldStorage,
} from '../lib/topicFolds';
import type { ConversationGroup } from '../lib/topicGroups';

/**
 * Which topics of the list are open. A topic follows a default (open when it holds the selected conversation or one
 * that needs attention) until the user folds or opens it: that choice then wins and survives a reload. The groups
 * given are all the topics, whatever the filter, so the defaults do not move when a filter is picked.
 */
export function useTopicFolds(
  groups: MaybeRefOrGetter<readonly ConversationGroup[]>,
  selectedId: MaybeRefOrGetter<string | null>,
  storage: FoldStorage | null = browserStorage(),
) {
  const overrides = ref<FoldOverrides>(readFolds(storage));
  const defaults = computed(() => defaultOpenKeys(toValue(groups), toValue(selectedId)));

  const save = (next: FoldOverrides): void => {
    overrides.value = next;
    writeFolds(storage, next);
  };

  // Ids of topics the core deleted or merged go away. Nothing is pruned while the list is empty: it may not have loaded.
  watch(
    () => toValue(groups).map((group) => group.key).join('\n'),
    () => {
      const keys = toValue(groups).map((group) => group.key);
      if (keys.length === 0) return;
      const pruned = pruneFolds(overrides.value, new Set(keys));
      if (pruned !== overrides.value) save(pruned);
    },
    { immediate: true, flush: 'sync' },
  );

  const isOpen = (key: string): boolean => isTopicOpen(key, overrides.value, defaults.value);
  const toggle = (key: string): void => save({ ...overrides.value, [key]: !isOpen(key) });
  /** Opens or folds every given topic, as an explicit choice. */
  const setAll = (keys: readonly string[], open: boolean): void => save({ ...overrides.value, ...Object.fromEntries(keys.map((key) => [key, open])) });
  const anyOpen = (keys: readonly string[]): boolean => keys.some(isOpen);

  return { isOpen, toggle, setAll, anyOpen };
}
