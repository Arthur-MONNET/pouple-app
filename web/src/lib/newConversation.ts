import type { NewConversation } from '../api/types';

/** The event the macOS shell sends to open the composer of a new conversation (menu bar, Cmd+N). It has no detail. */
export const NEW_CONVERSATION_EVENT = 'pouple:new-conversation';

/** The core refuses a longer message; it counts UTF-8 bytes (Go's len), so an accented letter counts twice. */
export const MAX_MESSAGE_BYTES = 4000;

/** What the user is writing, before it is sent. */
export interface ConversationDraft {
  text: string;
  /** Ids of the files already uploaded for this draft. */
  attachmentIds: readonly string[];
  /** The project folder picked, or null for none. */
  projectId: string | null;
}

/** Why a draft cannot be sent yet. The core checks the same rules: it stays the judge. */
export type DraftProblem = 'empty' | 'tooLong';

const encoder = new TextEncoder();

export const messageBytes = (text: string): number => encoder.encode(text.trim()).length;

export function draftProblem(draft: ConversationDraft): DraftProblem | null {
  const text = draft.text.trim();
  if (text === '' && draft.attachmentIds.length === 0) return 'empty';
  if (messageBytes(text) > MAX_MESSAGE_BYTES) return 'tooLong';
  return null;
}

/** The body of POST /api/assistant/requests: the trimmed text, the files, and the project only when one was picked. */
export function conversationPayload(draft: ConversationDraft): NewConversation {
  return {
    text: draft.text.trim(),
    attachmentIds: [...draft.attachmentIds],
    ...(draft.projectId ? { projectId: draft.projectId } : {}),
  };
}

type Listenable = Pick<EventTarget, 'addEventListener' | 'removeEventListener'>;

/** Calls the handler each time the shell asks for a new conversation; returns what stops listening. */
export function onNewConversationRequest(handler: () => void, target: Listenable = window): () => void {
  const listener = (): void => handler();
  target.addEventListener(NEW_CONVERSATION_EVENT, listener);
  return () => target.removeEventListener(NEW_CONVERSATION_EVENT, listener);
}
