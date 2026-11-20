import { describe, expect, it } from 'vitest';
import { createFixtures } from '../api/mockFixtures';
import type { AssistantPlan, AssistantRequest } from '../api/types';
import { approves, pendingDecision, pickedActions, planQuestions, proposalQuestions } from './requestDecisions';

const fixtures = createFixtures('en');
const base = fixtures.find((r) => r.status === 'ready') as AssistantRequest;
const plan = (actions: number, status: AssistantPlan['status'] = 'proposed'): AssistantPlan => ({
  id: 'p1',
  goal: 'Fix the footer',
  brief: 'Use the current year.',
  status,
  actions: Array.from({ length: actions }, (_, index) => ({
    kind: 'code.edit',
    scope: { service: 'code', id: `front-${index}`, name: 'Front' },
    summary: `Change ${index}`,
    branch: `fix/footer-${index}`,
    base: 'main',
  })),
});

describe('what waits for the user at the bottom of a conversation', () => {
  it('asks the questions first, then the plan, then the proposed change', () => {
    const asking = fixtures.find((r) => r.questions.length > 0) as AssistantRequest;
    expect(pendingDecision(asking)).toBe('questions');
    expect(pendingDecision({ ...base, questions: [], plan: plan(1) })).toBe('plan');
    expect(pendingDecision({ ...base, questions: [], plan: plan(1, 'approved') })).toBeNull();
  });

  it('turns a plan of several actions into one question where each action is ticked', () => {
    const [question] = planQuestions({ ...base, plan: plan(2) });
    expect(question?.multiple).toBe(true);
    expect(question?.options.map((option) => option.id)).toEqual(['action-0', 'action-1']);
    expect(pickedActions([{ questionId: 'plan', optionIds: ['action-1', 'action-0'] }])).toEqual([0, 1]);
  });

  it('asks a single action plan, and a proposed change, as yes or no', () => {
    expect(planQuestions({ ...base, plan: plan(1) })[0]?.options.map((option) => option.id)).toEqual(['approve', 'decline']);
    expect(proposalQuestions(base)).toEqual([]);
    expect(approves([{ questionId: 'plan', optionId: 'approve' }])).toBe(true);
    expect(approves([{ questionId: 'plan', optionId: 'decline' }])).toBe(false);
  });
});
