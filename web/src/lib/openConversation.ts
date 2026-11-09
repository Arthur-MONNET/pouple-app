/** The event the macOS shell sends to show one conversation (the bubble's "Open Pouple" button). */
export const OPEN_CONVERSATION_EVENT = 'pouple:open-conversation';

const idPattern = /^[A-Za-z0-9_-]{1,64}$/;

/** The conversation id an `open-conversation` event names, or null when it is not a plain id. */
export function conversationIdOf(event: Event): string | null {
  const id = (event as CustomEvent<{ id?: unknown } | null>).detail?.id;
  return typeof id === 'string' && idPattern.test(id) ? id : null;
}
