import type { AssistantRequest, AssistantTopic } from '../api/types';
import { t } from '../i18n';
import { headlineOf, isEvent, isInApp, needsAttention } from './assistantRequests';
import { isPinned } from './shelf';

/** The key of the trailing group of conversations the core has not sorted into a topic. */
export const UNSORTED_KEY = 'unsorted';
/** The key of the group at the top that holds the conversations the user pinned. */
export const PINNED_KEY = 'pinned';
/** The key of the group of conversations the user started in Pouple and the core has not sorted yet. */
export const IN_APP_KEY = 'in-app';

export interface ConversationGroup {
  /** The topic id, or `unsorted`. Stable across refreshes: what keeps a folded topic folded. */
  key: string;
  label: string;
  /** Newest activity first, as they came. */
  requests: AssistantRequest[];
  /** How many of them wait for a decision or failed. */
  attention: number;
}

/**
 * Groups requests by topic. Topics are ordered by their latest activity (the input is already newest first). A request
 * with no topic, or one the snapshot does not know, goes in one trailing `unsorted` group, except a conversation the
 * user started in Pouple: those go in their own `in-app` group, placed by its latest activity like a topic. The
 * conversations the user pinned come out of their topic into a `pinned` group that always comes first.
 */
export function groupByTopic(requests: readonly AssistantRequest[], topics: readonly AssistantTopic[]): ConversationGroup[] {
  const titles = new Map(topics.map((topic) => [topic.id, topic.title]));
  const groups = new Map<string, ConversationGroup>();
  const unsorted: ConversationGroup = { key: UNSORTED_KEY, label: t('list.unsorted'), requests: [], attention: 0 };
  const pinned: ConversationGroup = { key: PINNED_KEY, label: t('list.pinned'), requests: [], attention: 0 };
  for (const request of requests) {
    if (isPinned(request)) {
      pinned.requests.push(request);
      if (needsAttention(request)) pinned.attention += 1;
      continue;
    }
    const title = request.topicId ? titles.get(request.topicId) : undefined;
    let group = unsorted;
    if (!(request.topicId && title !== undefined) && isInApp(request)) {
      let mine = groups.get(IN_APP_KEY);
      if (!mine) {
        mine = { key: IN_APP_KEY, label: t('list.mine'), requests: [], attention: 0 };
        groups.set(IN_APP_KEY, mine);
      }
      group = mine;
    } else if (request.topicId && title !== undefined) {
      let known = groups.get(request.topicId);
      if (!known) {
        known = { key: request.topicId, label: title, requests: [], attention: 0 };
        groups.set(request.topicId, known);
      }
      group = known;
    }
    group.requests.push(request);
    if (needsAttention(request)) group.attention += 1;
  }
  const sorted = unsorted.requests.length > 0 ? [...groups.values(), unsorted] : [...groups.values()];
  return pinned.requests.length > 0 ? [pinned, ...sorted] : sorted;
}

/**
 * Where a conversation comes from: the title of its row. A channel names the channel and the sender, a direct
 * message the person, and the other origins say what they are. Events of a service (pipeline, Asana, merge request,
 * reminder) are named after the service, since nobody wrote them.
 */
export function sourceLabel(request: AssistantRequest): string {
  const { source } = request;
  switch (source.origin) {
    case 'note-to-self':
      return t('groups.notes');
    case 'pipeline':
      return t('groups.pipelines');
    case 'asana':
      return t('groups.asana');
    case 'merge-request':
      return t('groups.mergeRequests');
    case 'reminder':
      return t('groups.reminders');
    case 'pull-request':
      return t('groups.pullRequests');
    case 'in-app':
      return t('groups.inApp');
    case 'direct-message':
      return source.senderName;
    case 'group-message':
      return t('groups.group');
    default:
      return `#${source.channelName ?? source.channelId} · ${source.senderName}`;
  }
}

/** The one-line preview under the title: the message, or for an event of a service its headline. */
export const previewOf = (request: AssistantRequest): string => (isEvent(request.source.origin) ? headlineOf(request.text) : request.text);
