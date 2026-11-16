import { describe, expect, it } from 'vitest';
import type { AssistantRequest, AssistantTopic, RequestOrigin } from '../api/types';
import { groupByTopic, previewOf, sourceLabel, UNSORTED_KEY } from './topicGroups';

function request(id: string, origin: RequestOrigin = 'mention', topicId?: string, overrides: Partial<AssistantRequest['source']> = {}, status: AssistantRequest['status'] = 'ready'): AssistantRequest {
  return {
    id, text: `text ${id}`, context: '', receivedAt: '2026-11-09T10:00:00Z', updatedAt: '2026-11-09T10:00:00Z', status,
    summary: null, draftReply: null, questions: [], proposal: null, entries: [], error: null,
    ...(topicId ? { topicId } : {}),
    source: { origin, channelId: 'C1', channelName: 'tech-review', senderId: 'U1', senderName: 'Jocelyn', ts: '1.1', permalink: null, ...overrides },
  } as AssistantRequest;
}

const topic = (id: string, title: string): AssistantTopic => ({ id, title, createdAt: '2026-11-09T09:00:00Z', updatedAt: '2026-11-09T10:00:00Z' });
const TOPICS = [topic('t1', 'Billing'), topic('t2', 'Release')];

describe('groupByTopic', () => {
  it('groups by topic id, labelled with the title, in order of latest activity', () => {
    const groups = groupByTopic([request('a', 'mention', 't2'), request('b', 'mention', 't1'), request('c', 'direct-message', 't2')], TOPICS);
    expect(groups.map((group) => [group.key, group.label, group.requests.map((item) => item.id)])).toEqual([
      ['t2', 'Release', ['a', 'c']],
      ['t1', 'Billing', ['b']],
    ]);
  });

  it('puts requests with no topic, or an unknown one, in one trailing unsorted group', () => {
    const groups = groupByTopic([request('a'), request('b', 'mention', 't1'), request('c', 'mention', 'gone')], TOPICS);
    expect(groups.map((group) => group.key)).toEqual(['t1', UNSORTED_KEY]);
    expect(groups[1]?.requests.map((item) => item.id)).toEqual(['a', 'c']);
    expect(groups[1]?.label).toBeTruthy();
  });

  it('has no unsorted group when everything is sorted', () => {
    expect(groupByTopic([request('a', 'mention', 't1')], TOPICS).map((group) => group.key)).toEqual(['t1']);
  });

  it('works with no topics at all', () => {
    expect(groupByTopic([request('a', 'mention', 't1')], []).map((group) => group.key)).toEqual([UNSORTED_KEY]);
    expect(groupByTopic([], TOPICS)).toEqual([]);
  });

  it('keeps a topic whose title was renamed under the same key', () => {
    const before = groupByTopic([request('a', 'mention', 't1')], TOPICS)[0];
    const after = groupByTopic([request('a', 'mention', 't1')], [topic('t1', 'Invoices')])[0];
    expect([before?.key, after?.key, after?.label]).toEqual(['t1', 't1', 'Invoices']);
  });

  it('counts what waits for the user in each topic', () => {
    const groups = groupByTopic([request('a', 'mention', 't1', {}, 'awaiting-user'), request('b', 'mention', 't1', {}, 'failed'), request('c', 'mention', 't1'), request('d', 'mention', 't2'), request('e', 'mention', undefined, {}, 'failed')], TOPICS);
    expect(groups.map((group) => [group.key, group.attention])).toEqual([['t1', 2], ['t2', 0], [UNSORTED_KEY, 1]]);
  });
});

describe('sourceLabel', () => {
  it('names a channel and its sender, falling back to the channel id', () => {
    expect(sourceLabel(request('a', 'mention', undefined, { senderName: 'Samuel' }))).toBe('#tech-review · Samuel');
    expect(sourceLabel(request('b', 'thread-reply', undefined, { channelName: null, channelId: 'C77', senderName: 'Samuel' }))).toBe('#C77 · Samuel');
  });

  it('names a direct message after the person', () => {
    expect(sourceLabel(request('a', 'direct-message', undefined, { channelName: null, senderName: 'Nina' }))).toBe('Nina');
  });

  it('names a group conversation and a note generically', () => {
    expect(sourceLabel(request('a', 'group-message', undefined, { channelName: null }))).toMatch(/Group/i);
    expect(sourceLabel(request('b', 'note-to-self', undefined, { channelName: null }))).toMatch(/Notes/i);
  });

  it('names an event after its service', () => {
    expect(sourceLabel(request('a', 'pipeline'))).toMatch(/GitLab/);
    expect(sourceLabel(request('b', 'asana'))).toBe('Asana');
    expect(sourceLabel(request('c', 'merge-request'))).toMatch(/Merge/i);
    expect(sourceLabel(request('d', 'reminder'))).toMatch(/Reminders/i);
  });
});

describe('previewOf', () => {
  it('shows the message, and the headline of an event', () => {
    const pipeline = request('a', 'pipeline');
    pipeline.text = '❌ dating/vision · feat\nPipeline #1 failed';
    expect(previewOf(pipeline)).toBe('❌ dating/vision · feat');
    expect(previewOf(request('b', 'mention'))).toBe('text b');
  });
});
