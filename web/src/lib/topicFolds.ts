import type { ConversationGroup } from './topicGroups';

/** topic id → whether the user left it open (true) or folded it (false). A missing id follows the default. */
export type FoldOverrides = Record<string, boolean>;

export const FOLDS_KEY = 'pouple.topicFolds';

/** How many topics needing attention are open by default. */
export const MAX_OPEN_ATTENTION = 3;

/** How many conversations an open topic shows before "Show more". */
export const TOPIC_PAGE_SIZE = 5;

/** The few methods of Storage this file needs, so tests can pass a fake. */
export type FoldStorage = Pick<Storage, 'getItem' | 'setItem'>;

export const browserStorage = (): FoldStorage | null => {
  try {
    return globalThis.localStorage ?? null;
  } catch {
    return null;
  }
};

/** The saved overrides. Storage can be missing, throw, or hold garbage: the defaults then apply. */
export function readFolds(storage: FoldStorage | null): FoldOverrides {
  try {
    const parsed: unknown = JSON.parse(storage?.getItem(FOLDS_KEY) ?? '{}');
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return {};
    return Object.fromEntries(Object.entries(parsed).filter((entry): entry is [string, boolean] => typeof entry[1] === 'boolean'));
  } catch {
    return {};
  }
}

export function writeFolds(storage: FoldStorage | null, folds: FoldOverrides): void {
  try {
    storage?.setItem(FOLDS_KEY, JSON.stringify(folds));
  } catch {
    // The folds then last until the page closes, which is fine.
  }
}

/** Drops the overrides of topics that no longer exist. Returns the same object when nothing changes. */
export function pruneFolds(folds: FoldOverrides, existing: ReadonlySet<string>): FoldOverrides {
  const kept = Object.entries(folds).filter(([key]) => existing.has(key));
  return kept.length === Object.keys(folds).length ? folds : Object.fromEntries(kept);
}

/**
 * The topics open when the user said nothing: the one holding the selected conversation, and the most recent
 * topics (the groups come newest first) that hold a conversation needing attention.
 */
export function defaultOpenKeys(groups: readonly ConversationGroup[], selectedId: string | null): Set<string> {
  const open = new Set<string>();
  for (const group of groups) {
    if (selectedId && group.requests.some((request) => request.id === selectedId)) open.add(group.key);
  }
  let withAttention = 0;
  for (const group of groups) {
    if (group.attention > 0 && withAttention < MAX_OPEN_ATTENTION) {
      open.add(group.key);
      withAttention += 1;
    }
  }
  return open;
}

export const isTopicOpen = (key: string, overrides: FoldOverrides, defaults: ReadonlySet<string>): boolean => overrides[key] ?? defaults.has(key);

/**
 * What an open topic shows: its first conversations, or all of them once expanded. A folded topic shows only the
 * selected one. The selected conversation is always shown, wherever it sits, and `hidden` counts the rest.
 */
export function shownRequests<T extends { id: string }>(requests: readonly T[], open: boolean, expanded: boolean, selectedId: string | null): { shown: T[]; hidden: number } {
  const shown = requests.filter((request, index) => request.id === selectedId || (open && (expanded || index < TOPIC_PAGE_SIZE)));
  return { shown, hidden: open ? requests.length - shown.length : 0 };
}

