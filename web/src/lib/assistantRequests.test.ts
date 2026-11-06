import { describe, expect, it } from 'vitest';
import { createFixtures } from '../api/mockFixtures';
import type { AssistantStatus, ChatEntry } from '../api/types';
import { attentionCount, byRecentActivity, groupEntries, isWorking, matchesFilter, requestTitle } from './assistantRequests';

const ALL: AssistantStatus[] = ['queued', 'analyzing', 'awaiting-user', 'ready', 'applying', 'done', 'ignored', 'failed'];

describe('conversation filters', () => {
  it('puts what waits for a decision or failed under "Needs me"', () => {
    expect(ALL.filter((status) => matchesFilter({ status }, 'needs-me'))).toEqual(['awaiting-user', 'failed']);
  });

  it('puts what needs nothing more under "Done"', () => {
    expect(ALL.filter((status) => matchesFilter({ status }, 'done'))).toEqual(['ready', 'done', 'ignored']);
  });

  it('shows everything under "All"', () => {
    expect(ALL.every((status) => matchesFilter({ status }, 'all'))).toBe(true);
  });

  it('counts attention and working requests', () => {
    const fixtures = createFixtures('en');
    expect(attentionCount(fixtures)).toBe(3);
    expect(fixtures.filter(isWorking).map((r) => r.status).sort()).toEqual(['analyzing', 'applying', 'queued']);
  });
});

describe('requestTitle', () => {
  const fixtures = createFixtures('en');
  const titleOf = (origin: string): string => requestTitle(fixtures.find((r) => r.source.origin === origin) as (typeof fixtures)[number]);

  it('names the sender and where the message came from', () => {
    expect(titleOf('mention')).toMatch(/^Request from .+ in #/);
    expect(titleOf('direct-message')).toContain('(direct message)');
    expect(titleOf('note-to-self')).toBe('Note to self');
  });
});

describe('ordering and grouping', () => {
  it('lists the latest activity first', () => {
    const fixtures = createFixtures('en');
    const ordered = byRecentActivity(fixtures);
    for (let i = 1; i < ordered.length; i += 1) expect((ordered[i - 1] as (typeof ordered)[number]).updatedAt >= (ordered[i] as (typeof ordered)[number]).updatedAt).toBe(true);
  });

  it('merges consecutive work-log steps into one group', () => {
    const entry = (id: string, kind: ChatEntry['kind']): ChatEntry => ({ id, at: '', role: 'assistant', kind, text: id });
    const groups = groupEntries([entry('a', 'step'), entry('b', 'step'), entry('c', 'text'), entry('d', 'step')]);
    expect(groups.map((g) => g.type)).toEqual(['steps', 'message', 'steps']);
    expect(groups[0]).toMatchObject({ entries: [{ id: 'a' }, { id: 'b' }] });
  });
});
