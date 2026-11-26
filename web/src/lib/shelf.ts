import type { ArrangeChange, AssistantRequest } from '../api/types';

type Shelved = Pick<AssistantRequest, 'pinnedAt' | 'archivedAt' | 'snoozedUntil'>;

export const isPinned = (request: Shelved): boolean => Boolean(request.pinnedAt);
export const isArchived = (request: Shelved): boolean => Boolean(request.archivedAt);
/** The core clears the snooze when its moment passes, so a set date means the conversation is still postponed. */
export const isSnoozed = (request: Shelved): boolean => Boolean(request.snoozedUntil);
/** Put away, by the user or by itself, or postponed: out of the main list. */
export const isAway = (request: Shelved): boolean => isArchived(request) || isSnoozed(request);

export type SnoozePreset = 'hour' | 'tomorrow' | 'week';
export const SNOOZE_PRESETS: readonly SnoozePreset[] = ['hour', 'tomorrow', 'week'];

const MORNING = 9;

/** Where a snooze preset lands, as an RFC 3339 moment: in an hour, tomorrow at 9:00, or next Monday at 9:00 (local time). */
export function snoozeUntil(preset: SnoozePreset, now: Date): string {
  if (preset === 'hour') return new Date(now.getTime() + 60 * 60 * 1000).toISOString();
  const target = new Date(now.getFullYear(), now.getMonth(), now.getDate(), MORNING, 0, 0, 0);
  const days = preset === 'tomorrow' ? 1 : ((8 - now.getDay()) % 7 || 7);
  target.setDate(target.getDate() + days);
  return target.toISOString();
}

/**
 * The change that puts a conversation back as it was before another one. A snooze already past is not asked again:
 * the core refuses a date that is not in the future.
 */
export function undoOf(before: Shelved, now: Date): ArrangeChange {
  const snooze = before.snoozedUntil && new Date(before.snoozedUntil) > now ? before.snoozedUntil : '';
  return { pinned: isPinned(before), archived: isArchived(before), snoozedUntil: snooze };
}
