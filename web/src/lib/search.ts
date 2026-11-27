import type { AssistantRequest } from '../api/types';
import { isArchived, isSnoozed } from './shelf';
import { requestTitle } from './assistantRequests';
import { sourceLabel } from './topicGroups';

/**
 * Lower case without accents, one character for one character, so a position found in the folded text is the same
 * position in the original (a character that would change length stays as it is).
 */
export function fold(text: string): string {
  let out = '';
  for (const char of text) {
    const plain = char.normalize('NFD').replace(/\p{M}/gu, '').toLowerCase();
    out += plain.length === char.length ? plain : char.toLowerCase().length === char.length ? char.toLowerCase() : char;
  }
  return out;
}

/** The words of a query, folded. An empty query has none. */
export const termsOf = (query: string): string[] => fold(query).split(/\s+/).filter(Boolean);

export interface Excerpt {
  before: string;
  match: string;
  after: string;
}

export interface ConversationHit {
  request: AssistantRequest;
  title: string;
  /** Where the words were found, when it is not the title. */
  excerpt: Excerpt | null;
  state: 'archived' | 'snoozed' | null;
}

const CONTEXT = 36;

/** A window of text around the first of the words, cut at word edges and marked with an ellipsis. */
export function excerptOf(text: string, terms: readonly string[]): Excerpt | null {
  const flat = text.replace(/\s+/g, ' ');
  const folded = fold(flat);
  let best: { at: number; length: number } | null = null;
  for (const term of terms) {
    const at = folded.indexOf(term);
    if (at >= 0 && (best === null || at < best.at)) best = { at, length: term.length };
  }
  if (!best) return null;
  const start = Math.max(0, best.at - CONTEXT);
  const end = Math.min(flat.length, best.at + best.length + CONTEXT);
  return {
    before: (start > 0 ? '…' : '') + flat.slice(start, best.at).trimStart(),
    match: flat.slice(best.at, best.at + best.length),
    after: flat.slice(best.at + best.length, end).trimEnd() + (end < flat.length ? '…' : ''),
  };
}

/** The fields of a conversation a word can be found in, most telling first. */
function fieldsOf(request: AssistantRequest): { text: string; title: boolean }[] {
  return [
    { text: requestTitle(request), title: true },
    { text: sourceLabel(request), title: true },
    { text: request.summary ?? '', title: false },
    { text: request.text, title: false },
    ...request.entries.filter((entry) => entry.kind === 'text').map((entry) => ({ text: entry.text, title: false })),
  ];
}

/**
 * The conversations that hold every word (in any order, accents and case ignored), best first: a word in the title
 * counts more than one in a message, and equal scores keep the order they came in (the latest activity first). The
 * archive and the snoozed conversations are searched too, and say so.
 */
export function searchConversations(requests: readonly AssistantRequest[], query: string, limit = 8): ConversationHit[] {
  const terms = termsOf(query);
  if (terms.length === 0) return [];
  const hits: { hit: ConversationHit; score: number }[] = [];
  for (const request of requests) {
    const fields = fieldsOf(request).map((field) => ({ ...field, folded: fold(field.text) }));
    if (!terms.every((term) => fields.some((field) => field.folded.includes(term)))) continue;
    let score = 0;
    for (const term of terms) score += fields.some((field) => field.title && field.folded.includes(term)) ? 4 : 1;
    const inTitle = terms.every((term) => fields.some((field) => field.title && field.folded.includes(term)));
    const where = inTitle ? undefined : fields.find((field) => !field.title && terms.some((term) => field.folded.includes(term)));
    hits.push({
      score,
      hit: {
        request,
        title: requestTitle(request),
        excerpt: where ? excerptOf(where.text, terms) : null,
        state: isArchived(request) ? 'archived' : isSnoozed(request) ? 'snoozed' : null,
      },
    });
  }
  return hits
    .map((item, index) => ({ ...item, index }))
    .sort((a, b) => b.score - a.score || a.index - b.index)
    .slice(0, limit)
    .map((item) => item.hit);
}

/** Whether a command's label (or its keywords) holds every word of the query. */
export const matchesCommand = (haystack: string, query: string): boolean => {
  const folded = fold(haystack);
  return termsOf(query).every((term) => folded.includes(term));
};

/** What the palette shows before anything is typed: the latest conversations still in the main list. */
export const recentConversations = (requests: readonly AssistantRequest[], limit = 5): ConversationHit[] =>
  requests
    .filter((request) => !isArchived(request) && !isSnoozed(request))
    .slice(0, limit)
    .map((request) => ({ request, title: requestTitle(request), excerpt: null, state: null }));

/** The result an arrow key moves to: the list wraps around at both ends, and an empty list stays at the start. */
export const stepIndex = (current: number, count: number, direction: 1 | -1): number => (count === 0 ? 0 : (current + direction + count) % count);
