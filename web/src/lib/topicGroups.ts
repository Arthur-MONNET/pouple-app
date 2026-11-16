import type { AssistantRequest, AssistantTopic } from '../api/types';
import { t } from '../i18n';
import { headlineOf, needsAttention } from './assistantRequests';

/** The key of the trailing group of conversations the core has not sorted into a topic. */
export const UNSORTED_KEY = 'unsorted';

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
 * with no topic, or one the snapshot does not know, goes in one trailing `unsorted` group.
 */
export function groupByTopic(requests: readonly AssistantRequest[], topics: readonly AssistantTopic[]): ConversationGroup[] {
  const titles = new Map(topics.map((topic) => [topic.id, topic.title]));
  const groups = new Map<string, ConversationGroup>();
  const unsorted: ConversationGroup = { key: UNSORTED_KEY, label: t('list.unsorted'), requests: [], attention: 0 };
  for (const request of requests) {
    const title = request.topicId ? titles.get(request.topicId) : undefined;
    let group = unsorted;
    if (request.topicId && title !== undefined) {
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
  return unsorted.requests.length > 0 ? [...groups.values(), unsorted] : [...groups.values()];
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
    case 'direct-message':
      return source.senderName;
    case 'group-message':
      return t('groups.group');
    default:
      return `#${source.channelName ?? source.channelId} · ${source.senderName}`;
  }
}

const EVENT_ORIGINS: readonly AssistantRequest['source']['origin'][] = ['pipeline', 'asana', 'merge-request', 'reminder'];

/** The one-line preview under the title: the message, or for an event of a service its headline. */
export const previewOf = (request: AssistantRequest): string =>
  EVENT_ORIGINS.includes(request.source.origin) ? headlineOf(request.text) : request.text;
