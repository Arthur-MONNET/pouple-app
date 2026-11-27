import { describe, expect, it } from 'vitest';
import type { AssistantRequest } from '../api/types';
import { excerptOf, fold, matchesCommand, recentConversations, searchConversations, stepIndex, termsOf } from './search';

function request(id: string, patch: Partial<AssistantRequest> = {}, text = ''): AssistantRequest {
  return {
    id, text, context: '', receivedAt: '2026-11-09T10:00:00Z', updatedAt: '2026-11-09T10:00:00Z', status: 'ready',
    summary: null, draftReply: null, questions: [], proposal: null, entries: [], error: null,
    source: { origin: 'mention', channelId: 'C1', channelName: 'front', senderId: 'U1', senderName: 'Nadia', ts: '1.1', permalink: null },
    ...patch,
  } as AssistantRequest;
}

const entry = (text: string, kind: 'text' | 'step' = 'text') => ({ id: text, at: '2026-11-09T10:00:00Z', role: 'assistant' as const, kind, text });

describe('folding', () => {
  it('ignores case and accents without moving a single position', () => {
    expect(fold('Réunion À PARIS')).toBe('reunion a paris');
    expect(fold('Éléphant').length).toBe('Éléphant'.length);
  });

  it('splits a query into folded words', () => {
    expect(termsOf('  Réunion   Paris ')).toEqual(['reunion', 'paris']);
    expect(termsOf('   ')).toEqual([]);
  });
});

describe('searching conversations', () => {
  it('finds a word in a message of the conversation, accents ignored, and shows where', () => {
    const found = searchConversations([request('a', { entries: [entry('Le déploiement de staging a échoué hier soir')] })], 'DEPLOIEMENT');
    expect(found).toHaveLength(1);
    expect(found[0]?.excerpt?.match).toBe('déploiement');
    expect(found[0]?.excerpt?.after).toContain('staging');
  });

  it('wants every word, in any order, though not in the same message', () => {
    const one = request('a', { summary: 'The webhook secret leaked', entries: [entry('We rotated the key')] });
    expect(searchConversations([one], 'rotated webhook')).toHaveLength(1);
    expect(searchConversations([one], 'rotated invoice')).toEqual([]);
  });

  it('does not read the steps of the work log', () => {
    expect(searchConversations([request('a', { entries: [entry('git push --force', 'step')] })], 'force')).toEqual([]);
  });

  it('ranks a word in the title above a word in a message and keeps the latest first on a tie', () => {
    const other = { origin: 'mention', channelId: 'C1', channelName: 'front', senderId: 'U2', senderName: 'Morgan', ts: '1.2', permalink: null } as const;
    const inMessage = request('message', { source: other, entries: [entry('about nadia')] }, 'x');
    const inTitle = request('title'); // The sender Nadia is part of the title.
    const second = request('title-2');
    expect(searchConversations([inMessage, inTitle, second], 'nadia').map((hit) => hit.request.id)).toEqual(['title', 'title-2', 'message']);
  });

  it('searches the archive and the snoozed ones, and says so', () => {
    const hits = searchConversations([request('a', { archivedAt: 'x' }), request('b', { snoozedUntil: 'y' }), request('c')], 'nadia');
    expect(hits.map((hit) => hit.state)).toEqual(['archived', 'snoozed', null]);
  });

  it('returns nothing for an empty query and caps the number of results', () => {
    expect(searchConversations([request('a')], '  ')).toEqual([]);
    const many = Array.from({ length: 20 }, (_, index) => request(`r${index}`));
    expect(searchConversations(many, 'nadia', 5)).toHaveLength(5);
  });
});

describe('excerpts', () => {
  it('cuts around the word with ellipses, and nothing when it is absent', () => {
    const text = `${'a'.repeat(60)} needle ${'b'.repeat(60)}`;
    const excerpt = excerptOf(text, ['needle']);
    expect(excerpt?.match).toBe('needle');
    expect(excerpt?.before.startsWith('…')).toBe(true);
    expect(excerpt?.after.endsWith('…')).toBe(true);
    expect(excerptOf('nothing here', ['needle'])).toBeNull();
  });

  it('keeps the original spelling of the match', () => {
    expect(excerptOf('Déjà vu', ['deja'])?.match).toBe('Déjà');
  });
});

describe('commands', () => {
  it('match every word of the query in the label, accents ignored', () => {
    expect(matchesCommand('Réglages settings', 'regl')).toBe(true);
    expect(matchesCommand('Réglages settings', 'regl mémoire')).toBe(false);
    expect(matchesCommand('anything', '')).toBe(true);
  });
});

describe('moving through the results', () => {
  it('wraps around at both ends and stays put on an empty list', () => {
    expect(stepIndex(0, 3, 1)).toBe(1);
    expect(stepIndex(2, 3, 1)).toBe(0);
    expect(stepIndex(0, 3, -1)).toBe(2);
    expect(stepIndex(0, 0, 1)).toBe(0);
  });
});

describe('before anything is typed', () => {
  it('offers the latest conversations of the main list, not the ones put away', () => {
    const list = [request('a', { archivedAt: 'x' }), request('b'), request('c', { snoozedUntil: 'y' }), request('d')];
    expect(recentConversations(list, 5).map((hit) => hit.request.id)).toEqual(['b', 'd']);
    expect(recentConversations(Array.from({ length: 9 }, (_, i) => request(`r${i}`)), 5)).toHaveLength(5);
  });
});
