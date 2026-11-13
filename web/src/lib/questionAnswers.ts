import type { AssistantQuestion, QuestionAnswer } from '../api/types';

/** The choice that stands for the typed answer on a single-choice question. */
export const OTHER = '__other';

/** What the user did on one question so far. */
export interface QuestionDraft {
  /** The option picked on a single-choice question, or OTHER for the typed answer. */
  choice?: string;
  /** The options picked on a question that allows several. */
  picks: string[];
  /** The typed answer. */
  other: string;
}

export const emptyDraft = (): QuestionDraft => ({ picks: [], other: '' });

/** Picks an option that was not picked, and unpicks one that was, keeping the order the options are listed in. */
export function togglePick(question: AssistantQuestion, draft: QuestionDraft, optionId: string): QuestionDraft {
  const picked = draft.picks.includes(optionId) ? draft.picks.filter((id) => id !== optionId) : [...draft.picks, optionId];
  const listed = question.options.map((option) => option.id).filter((id) => picked.includes(id));
  return { ...draft, picks: listed };
}

/** Whether the draft answers its question: an option, several options, or a typed text. */
export function isAnswered(question: AssistantQuestion, draft: QuestionDraft): boolean {
  const typed = draft.other.trim() !== '';
  if (question.multiple) return draft.picks.length > 0 || typed;
  return draft.choice === OTHER ? typed : draft.choice !== undefined;
}

/** The answer the core expects: one option, several options plus what was typed, or only what was typed. */
export function toAnswer(question: AssistantQuestion, draft: QuestionDraft): QuestionAnswer {
  const text = draft.other.trim();
  if (question.multiple) {
    return { questionId: question.id, optionIds: [...draft.picks], ...(text ? { text } : {}) };
  }
  return draft.choice === OTHER ? { questionId: question.id, text } : { questionId: question.id, optionId: draft.choice as string };
}
