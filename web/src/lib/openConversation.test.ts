import { describe, expect, it } from 'vitest';
import { conversationIdOf, OPEN_CONVERSATION_EVENT } from './openConversation';

const eventWith = (detail: unknown): Event => new CustomEvent(OPEN_CONVERSATION_EVENT, { detail });

describe('conversationIdOf', () => {
  it('reads a plain conversation id', () => {
    expect(conversationIdOf(eventWith({ id: 'a1b2c3d4e5f60718' }))).toBe('a1b2c3d4e5f60718');
  });

  it('refuses anything that is not a plain id, so it can never become another route', () => {
    for (const id of ['', '../settings', 'a/b', 'a?b=1', 'a b', 'x'.repeat(65), 12, null, undefined, { id: 'x' }]) {
      expect(conversationIdOf(eventWith({ id }))).toBeNull();
    }
    expect(conversationIdOf(eventWith(null))).toBeNull();
    expect(conversationIdOf(new Event(OPEN_CONVERSATION_EVENT))).toBeNull();
  });
});
