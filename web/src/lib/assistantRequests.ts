import type { AssistantRequest, AssistantStatus, ChatEntry } from '../api/types';

const WORKING = new Set<AssistantStatus>(['queued', 'analyzing', 'applying']);

export const STATUS_LABELS: Record<AssistantStatus, string> = {
  queued: 'Queued',
  analyzing: 'Investigating…',
  applying: 'Working on it…',
  'awaiting-user': 'Waiting for you',
  ready: 'Analysis ready',
  done: 'Change applied',
  ignored: 'Ignored',
  failed: 'Failed',
};

export const isWorking = (request: Pick<AssistantRequest, 'status'>): boolean => WORKING.has(request.status);

export const needsAttention = (request: Pick<AssistantRequest, 'status'>): boolean =>
  request.status === 'awaiting-user' || request.status === 'failed';

export function attentionCount(requests: readonly Pick<AssistantRequest, 'status'>[]): number {
  return requests.filter(needsAttention).length;
}

export type ConversationFilter = 'all' | 'needs-me' | 'done';

export const FILTER_LABELS: Record<ConversationFilter, string> = { all: 'All', 'needs-me': 'Needs me', done: 'Done' };

/** "Needs me" is what waits for a decision or failed; "Done" is everything that needs nothing more. */
export function matchesFilter(request: Pick<AssistantRequest, 'status'>, filter: ConversationFilter): boolean {
  if (filter === 'all') return true;
  if (filter === 'needs-me') return needsAttention(request);
  return request.status === 'ready' || request.status === 'done' || request.status === 'ignored';
}

/** The list shows the latest activity first. */
export const byRecentActivity = (requests: readonly AssistantRequest[]): AssistantRequest[] =>
  [...requests].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));

/** "Request from Morgan in #front", "... (direct message)" or "Note to self". */
export function requestTitle(request: Pick<AssistantRequest, 'source'>): string {
  const { source } = request;
  const channel = `#${source.channelName ?? source.channelId}`;
  switch (source.origin) {
    case 'note-to-self':
      return 'Note to self';
    case 'direct-message':
      return `Request from ${source.senderName} (direct message)`;
    case 'group-message':
      return `Message from ${source.senderName} (group chat)`;
    case 'broadcast':
      return `@here / @channel from ${source.senderName} in ${channel}`;
    case 'keyword':
      return `Watched word from ${source.senderName} in ${channel}`;
    case 'thread-reply':
      return `Thread reply from ${source.senderName} in ${channel}`;
    case 'channel-message':
      return `Message from ${source.senderName} in ${channel}`;
    default:
      return `Request from ${source.senderName} in ${channel}`;
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
