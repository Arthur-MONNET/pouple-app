import type { AssistantRequest } from '../api/types';
import { t } from '../i18n';
import { needsAttention } from './assistantRequests';

/** What kind of place the requests of a group come from. */
export type GroupKind = 'notes' | 'channel' | 'direct' | 'group';

export interface ConversationGroup {
  /** Stable across refreshes: what keeps a collapsed group collapsed. */
  key: string;
  kind: GroupKind;
  label: string;
  /** Newest activity first, as they came. */
  requests: AssistantRequest[];
  /** How many of them wait for a decision or failed. */
  attention: number;
}

const NOTES_KEY = 'notes';

function kindOf(request: AssistantRequest): GroupKind {
  switch (request.source.origin) {
    case 'note-to-self':
      return 'notes';
    case 'direct-message':
      return 'direct';
    case 'group-message':
      return 'group';
    default:
      return 'channel';
  }
}

const keyOf = (request: AssistantRequest): string =>
  kindOf(request) === 'notes' ? NOTES_KEY : `${kindOf(request)}:${request.source.channelId}`;

function labelOf(request: AssistantRequest): string {
  const { source } = request;
  switch (kindOf(request)) {
    case 'notes':
      return t('groups.notes');
    case 'direct':
      return source.senderName;
    case 'group':
      return t('groups.group');
    default:
      return `#${source.channelName ?? source.channelId}`;
  }
}

/**
 * Groups requests by where they came from, like a project in a sidebar: one group per Slack channel, direct
 * message conversation and group conversation, plus the notes to oneself. The groups are ordered by their latest
 * activity (the input is already newest first), and a group is named after its latest request, so a renamed channel
 * shows its new name.
 */
export function groupBySource(requests: readonly AssistantRequest[]): ConversationGroup[] {
  const groups = new Map<string, ConversationGroup>();
  for (const request of requests) {
    const key = keyOf(request);
    let group = groups.get(key);
    if (!group) {
      group = { key, kind: kindOf(request), label: labelOf(request), requests: [], attention: 0 };
      groups.set(key, group);
    }
    group.requests.push(request);
    if (needsAttention(request)) group.attention += 1;
  }
  return [...groups.values()];
}

/** In a channel or a group conversation several people write: the sender names the request. */
const namesTheSender = (kind: GroupKind): boolean => kind === 'channel' || kind === 'group';

/**
 * The line that names a request inside its group. The group already says where it comes from, and for a direct
 * conversation or a note who wrote it too: the title is then the message itself, and nothing repeats it below.
 */
export function itemTitle(request: AssistantRequest): string {
  return namesTheSender(kindOf(request)) ? request.source.senderName : request.text;
}

/** Whether the message is shown under the title (it already is the title otherwise). */
export const showsMessageBelow = (group: Pick<ConversationGroup, 'kind'>): boolean => namesTheSender(group.kind);

/** The group a request belongs to. */
export const groupKeyOf = keyOf;
