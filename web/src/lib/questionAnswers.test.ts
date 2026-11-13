import { describe, expect, it } from 'vitest';
import type { AssistantQuestion } from '../api/types';
import { emptyDraft, isAnswered, OTHER, toAnswer, togglePick } from './questionAnswers';

const options = [
  { id: 'A', label: 'Reply to Estelle', description: null },
  { id: 'B', label: 'Comment the task', description: null },
  { id: 'C', label: 'Open a merge request', description: null },
];
const single: AssistantQuestion = { id: 'q1', text: 'Which flow?', options };
const several: AssistantQuestion = { id: 'q2', text: 'What should I do?', options, multiple: true };

describe('single-choice questions', () => {
  it('are answered by one option', () => {
    const draft = { ...emptyDraft(), choice: 'B' };
    expect(isAnswered(single, draft)).toBe(true);
    expect(toAnswer(single, draft)).toEqual({ questionId: 'q1', optionId: 'B' });
  });

  it('are answered by the typed text only when something is typed', () => {
    expect(isAnswered(single, { ...emptyDraft(), choice: OTHER, other: '  ' })).toBe(false);
    const draft = { ...emptyDraft(), choice: OTHER, other: ' Lyon ' };
    expect(toAnswer(single, draft)).toEqual({ questionId: 'q1', text: 'Lyon' });
  });

  it('are not answered before a choice', () => {
    expect(isAnswered(single, emptyDraft())).toBe(false);
  });
});

describe('questions that allow several options', () => {
  it('toggle options and keep them in the listed order', () => {
    let draft = togglePick(several, emptyDraft(), 'C');
    draft = togglePick(several, draft, 'A');
    expect(draft.picks).toEqual(['A', 'C']);
    expect(togglePick(several, draft, 'C').picks).toEqual(['A']);
  });

  it('send the picked options with what was typed', () => {
    const draft = { ...togglePick(several, emptyDraft(), 'B'), other: ' and tell Florian ' };
    expect(toAnswer(several, draft)).toEqual({ questionId: 'q2', optionIds: ['B'], text: 'and tell Florian' });
  });

  it('are answered by options, by a typed text, or both, but not by nothing', () => {
    expect(isAnswered(several, emptyDraft())).toBe(false);
    expect(isAnswered(several, togglePick(several, emptyDraft(), 'A'))).toBe(true);
    expect(isAnswered(several, { ...emptyDraft(), other: 'something else' })).toBe(true);
    expect(toAnswer(several, togglePick(several, emptyDraft(), 'A'))).toEqual({ questionId: 'q2', optionIds: ['A'] });
  });
});
