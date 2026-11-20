import type { AssistantRequest, AssistantStatus, ChatEntry, RequestOrigin } from '../api/types';
import { t } from '../i18n';

const WORKING = new Set<AssistantStatus>(['queued', 'analyzing', 'applying']);

/** The label of a status, in the active language. */
export const statusLabel = (status: AssistantStatus): string => t(`status.${status}`);

export const isWorking = (request: Pick<AssistantRequest, 'status'>): boolean => WORKING.has(request.status);

export const needsAttention = (request: Pick<AssistantRequest, 'status'>): boolean =>
  request.status === 'awaiting-user' || request.status === 'failed';

export function attentionCount(requests: readonly Pick<AssistantRequest, 'status'>[]): number {
  return requests.filter(needsAttention).length;
}

export type ConversationFilter = 'all' | 'needs-me' | 'done';

export const FILTERS: readonly ConversationFilter[] = ['all', 'needs-me', 'done'];
export const filterLabel = (filter: ConversationFilter): string => t(`filters.${filter}`);

/** "Needs me" is what waits for a decision or failed; "Done" is everything that needs nothing more. */
export function matchesFilter(request: Pick<AssistantRequest, 'status'>, filter: ConversationFilter): boolean {
  if (filter === 'all') return true;
  if (filter === 'needs-me') return needsAttention(request);
  return request.status === 'ready' || request.status === 'done' || request.status === 'ignored';
}

/** The list shows the latest activity first. */
export const byRecentActivity = (requests: readonly AssistantRequest[]): AssistantRequest[] =>
  [...requests].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));

/** The first line of a text: what a pipeline request puts forward ("❌ group/project · branch"). */
export const headlineOf = (text: string): string => text.split('\n', 1)[0]?.trim() ?? '';

/** Origins where nobody wrote: a service reports something, and the first line of its text says what. */
const EVENT_ORIGINS: ReadonlySet<RequestOrigin> = new Set<RequestOrigin>(['pipeline', 'asana', 'merge-request', 'reminder', 'pull-request']);

export const isEvent = (origin: RequestOrigin): boolean => EVENT_ORIGINS.has(origin);

/** A conversation the user started in Pouple: their own words, never an event. */
export const isInApp = (request: Pick<AssistantRequest, 'source'>): boolean => request.source.origin === 'in-app';

/** "Demande de Morgan dans #front", "Note à moi-même", the first line of an event or of a conversation started in Pouple… */
export function requestTitle(request: Pick<AssistantRequest, 'source' | 'text'>): string {
  const { source } = request;
  if (isEvent(source.origin)) return headlineOf(request.text);
  if (source.origin === 'in-app') return source.label?.trim() || headlineOf(request.text) || t('title.in-app');
  return t(`title.${source.origin}`, { name: source.senderName, channel: source.channelName ?? source.channelId });
}

/** The i18n key of the link to the original (the permalink): where it opens depends on who sent the request. */
export function permalinkLabelKey(origin: RequestOrigin): string {
  switch (origin) {
    case 'pull-request':
      return 'chat.openInGitHub';
    case 'merge-request':
    case 'pipeline':
      return 'chat.openInGitLab';
    case 'asana':
      return 'chat.openInAsana';
    default:
      return 'chat.openInSlack';
  }
}

export type EntryGroup = { type: 'steps'; entries: ChatEntry[] } | { type: 'message'; entry: ChatEntry };

/** Consecutive work-log entries become one activity list; messages stay on their own. */
export function groupEntries(entries: readonly ChatEntry[]): EntryGroup[] {
  const groups: EntryGroup[] = [];
  for (const entry of entries) {
    const last = groups.at(-1);
    if (entry.kind === 'step') {
      if (last?.type === 'steps') last.entries.push(entry);
      else groups.push({ type: 'steps', entries: [entry] });
    } else {
      groups.push({ type: 'message', entry });
    }
  }
  return groups;
}
