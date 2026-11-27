import { describe, expect, it, vi } from 'vitest';
import {
  conversationPayload,
  draftProblem,
  MAX_MESSAGE_BYTES,
  NEW_CONVERSATION_EVENT,
  onNewConversationRequest,
  type ConversationDraft,
} from './newConversation';

const draft = (overrides: Partial<ConversationDraft> = {}): ConversationDraft => ({ text: '', attachmentIds: [], projectId: null, ...overrides });

describe('draftProblem', () => {
  it('refuses an empty or whitespace-only text without files', () => {
    expect(draftProblem(draft())).toBe('empty');
    expect(draftProblem(draft({ text: '  \n\t ' }))).toBe('empty');
  });

  it('accepts files alone, and text alone', () => {
    expect(draftProblem(draft({ text: ' ', attachmentIds: ['a1'] }))).toBeNull();
    expect(draftProblem(draft({ text: 'Why is the build red?' }))).toBeNull();
  });

  it('refuses a text over 4000 characters, as the core does', () => {
    expect(draftProblem(draft({ text: 'a'.repeat(MAX_MESSAGE_BYTES) }))).toBeNull();
    expect(draftProblem(draft({ text: 'a'.repeat(MAX_MESSAGE_BYTES + 1) }))).toBe('tooLong');
  });

  it('counts accented letters the way the core does (UTF-8 bytes), and ignores the surrounding blanks', () => {
    expect(draftProblem(draft({ text: 'é'.repeat(2001) }))).toBe('tooLong');
    expect(draftProblem(draft({ text: `  ${'a'.repeat(MAX_MESSAGE_BYTES)}  ` }))).toBeNull();
  });
});

describe('conversationPayload', () => {
  it('omits the project when none is picked', () => {
    expect(conversationPayload(draft({ text: ' hi ' }))).toEqual({ text: 'hi', attachmentIds: [] });
    expect('projectId' in conversationPayload(draft({ text: 'hi', projectId: '' }))).toBe(false);
  });

  it('carries the files and the picked project', () => {
    const ids = ['a1', 'a2'];
    const payload = conversationPayload(draft({ text: 'look', attachmentIds: ids, projectId: 'front' }));
    expect(payload).toEqual({ text: 'look', attachmentIds: ['a1', 'a2'], projectId: 'front' });
    expect(payload.attachmentIds).not.toBe(ids);
  });
});

describe('shell event', () => {
  it('calls the handler on each shell request until it stops listening', () => {
    const target = new EventTarget();
    const handler = vi.fn();
    const stop = onNewConversationRequest(handler, target);
    target.dispatchEvent(new Event(NEW_CONVERSATION_EVENT));
    target.dispatchEvent(new Event(NEW_CONVERSATION_EVENT));
    stop();
    target.dispatchEvent(new Event(NEW_CONVERSATION_EVENT));
    expect(handler).toHaveBeenCalledTimes(2);
  });
});
