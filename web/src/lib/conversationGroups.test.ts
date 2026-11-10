import { describe, expect, it } from 'vitest';
import type { AssistantRequest, RequestOrigin } from '../api/types';
import { groupBySource, groupKeyOf, itemTitle, showsMessageBelow } from './conversationGroups';

function request(id: string, origin: RequestOrigin, overrides: Partial<AssistantRequest['source']> = {}, status: AssistantRequest['status'] = 'ready'): AssistantRequest {
  return {
    id, text: `text ${id}`, context: '', receivedAt: '2026-11-09T10:00:00Z', updatedAt: '2026-11-09T10:00:00Z', status,
    summary: null, draftReply: null, questions: [], proposal: null, entries: [], error: null,
    source: { origin, channelId: 'C1', channelName: 'tech-review', senderId: 'U1', senderName: 'Jocelyn', ts: '1.1', permalink: null, ...overrides },
  } as AssistantRequest;
}

describe('groupBySource', () => {
  it('groups requests by channel, keeping the order of first appearance', () => {
    const groups = groupBySource([
      request('a', 'mention'),
      request('b', 'mention', { channelId: 'C2', channelName: 'front' }),
      request('c', 'thread-reply'),
    ]);
    expect(groups.map((group) => [group.label, group.requests.map((item) => item.id)])).toEqual([
      ['#tech-review', ['a', 'c']],
      ['#front', ['b']],
    ]);
  });

  it('puts every note to oneself in one group, whatever the channel', () => {
    const groups = groupBySource([request('a', 'note-to-self', { channelId: 'D1' }), request('b', 'note-to-self', { channelId: 'D2' })]);
    expect(groups).toHaveLength(1);
    expect(groups[0]?.kind).toBe('notes');
    expect(groups[0]?.requests).toHaveLength(2);
  });

  it('names a direct conversation after the person and a group conversation generically', () => {
    const groups = groupBySource([
      request('a', 'direct-message', { channelId: 'D9', channelName: null, senderName: 'Nina' }),
      request('b', 'group-message', { channelId: 'G1', channelName: null }),
    ]);
    expect(groups.map((group) => [group.kind, group.label])).toEqual([['direct', 'Nina'], ['group', expect.any(String)]]);
  });

  it('never mixes a channel and a direct conversation that share an id', () => {
    const a = request('a', 'mention', { channelId: 'X' });
    const b = request('b', 'direct-message', { channelId: 'X', channelName: null });
    expect(groupKeyOf(a)).not.toBe(groupKeyOf(b));
    expect(groupBySource([a, b])).toHaveLength(2);
  });

  it('puts every pipeline in one group named after GitLab, whatever the project, titled by its headline', () => {
    const first = request('a', 'pipeline', { channelId: 'gitlab-pipelines', channelName: 'GitLab', senderName: 'GitLab' });
    const second = request('b', 'pipeline', { channelId: 'gitlab-pipelines', channelName: 'GitLab', senderName: 'GitLab' });
    first.text = '❌ dating/vision · feat\nPipeline #1 failed';
    const groups = groupBySource([first, second]);
    expect(groups).toHaveLength(1);
    expect(groups[0]?.kind).toBe('pipelines');
    expect(groups[0]?.label).toMatch(/GitLab/);
    expect(itemTitle(first)).toBe('❌ dating/vision · feat');
    expect(showsMessageBelow(groups[0]!)).toBe(false);
  });

  it('counts what waits for the user', () => {
    const [group] = groupBySource([request('a', 'mention', {}, 'awaiting-user'), request('b', 'mention', {}, 'failed'), request('c', 'mention')]);
    expect(group?.attention).toBe(2);
  });

  it('falls back to the channel id when a channel has no name', () => {
    expect(groupBySource([request('a', 'mention', { channelName: null, channelId: 'C77' })])[0]?.label).toBe('#C77');
  });

  it('titles a request by its sender in a channel, and by its message in a direct conversation or a note', () => {
    expect(itemTitle(request('a', 'mention', { senderName: 'Samuel' }))).toBe('Samuel');
    expect(itemTitle(request('b', 'note-to-self'))).toBe('text b');
    expect(itemTitle(request('c', 'direct-message', { senderName: 'Nina' }))).toBe('text c');
  });

  it('shows the message under the title only when the title is the sender', () => {
    const kinds = groupBySource([request('a', 'mention'), request('b', 'direct-message', { channelId: 'D1' }), request('c', 'note-to-self'), request('d', 'group-message', { channelId: 'G1' })]);
    expect(kinds.map((group) => [group.kind, showsMessageBelow(group)])).toEqual([['channel', true], ['direct', false], ['notes', false], ['group', true]]);
  });
});
