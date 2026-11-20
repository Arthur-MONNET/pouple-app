import { describe, expect, it } from 'vitest';
import { createFixtures } from '../api/mockFixtures';
import type { AssistantStatus, ChatEntry } from '../api/types';
import {
  attentionCount,
  byRecentActivity,
  groupEntries,
  isEvent,
  isWorking,
  leavesAfterRemoval,
  matchesFilter,
  permalinkLabelKey,
  requestTitle,
} from './assistantRequests';

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

  it('titles an in-app conversation with its label, then its first line, then a generic name', () => {
    const base = { text: 'Why is the build red?\nSince this morning.', source: { origin: 'in-app', channelId: 'in-app:r1', channelName: null, senderId: 'self', senderName: '', ts: '1.0', permalink: null } } as const;
    expect(requestTitle({ ...base, source: { ...base.source, label: 'Build is red' } })).toBe('Build is red');
    expect(requestTitle(base)).toBe('Why is the build red?');
    expect(requestTitle({ ...base, text: '' })).toBe('Conversation');
  });

  it('titles a pull request event with its headline, like the other events', () => {
    const request = { text: 'octo/app #12 · New review\nLooks good.', source: { origin: 'pull-request', channelId: 'github-pull-requests', channelName: 'GitHub', senderId: 'github', senderName: 'GitHub', ts: '1.0', permalink: null } } as const;
    expect(requestTitle(request)).toBe('octo/app #12 · New review');
    expect(isEvent('pull-request')).toBe(true);
    expect(isEvent('in-app')).toBe(false);
  });

  it('opens the original in the service that sent it', () => {
    expect(permalinkLabelKey('pull-request')).toBe('chat.openInGitHub');
    expect(permalinkLabelKey('merge-request')).toBe('chat.openInGitLab');
    expect(permalinkLabelKey('asana')).toBe('chat.openInAsana');
    expect(permalinkLabelKey('mention')).toBe('chat.openInSlack');
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

describe('leavesAfterRemoval', () => {
  it('leaves the conversation that was deleted while it is shown', () => {
    expect(leavesAfterRemoval('a1', 'a1')).toBe(true);
  });

  it('stays on the conversation the user opened while another one was being deleted', () => {
    expect(leavesAfterRemoval('a1', 'b2')).toBe(false);
    expect(leavesAfterRemoval('a1', null)).toBe(false);
  });
});
