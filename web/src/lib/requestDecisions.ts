import type { AssistantQuestion, AssistantRequest, PlanAction, QuestionAnswer } from '../api/types';
import { t } from '../i18n';

/** What a plan's action is called where the user picks it: its kind, and the branches it names. */
export function planActionLabel(action: PlanAction): string {
  if (action.kind === 'asana.create-task') return `${action.title} · ${action.scope.name || action.scope.id}`;
  if (action.kind === 'slite.write') return `${action.title} · ${action.scope.id}`;
  const arrow = action.kind === 'gitlab.merge-request' ? '→' : '←';
  const where = action.branch ? (action.base ? `${action.branch} ${arrow} ${action.base}` : action.branch) : action.scope.id;
  return `${t(`chat.plan.kind.${action.kind}`)} · ${where}`;
}

/** Approving a change is asked like any other question, so it looks and behaves the same. */
export function proposalQuestions(request: AssistantRequest): AssistantQuestion[] {
  const { proposal } = request;
  if (!proposal) return [];
  return [
    {
      id: 'proposal',
      text: t('chat.proposalQuestion', { project: proposal.projectId, title: proposal.title }),
      options: [
        { id: 'approve', label: t('chat.proposalYes'), description: proposal.description },
        { id: 'decline', label: t('chat.proposalNo'), description: null },
      ],
    },
  ];
}

/** A plan with several actions lets the user tick the ones they want: only those are carried out. */
export const planIsPicked = (request: AssistantRequest): boolean => (request.plan?.actions.length ?? 0) > 1;

/** Approving a plan is asked like any other question, so it looks and behaves the same. */
export function planQuestions(request: AssistantRequest): AssistantQuestion[] {
  const { plan } = request;
  if (!plan || plan.status !== 'proposed') return [];
  if (planIsPicked(request)) {
    return [
      {
        id: 'plan',
        text: t('chat.planPickQuestion', { goal: plan.goal }),
        multiple: true,
        options: plan.actions.map((action, index) => ({ id: `action-${index}`, label: planActionLabel(action), description: action.summary })),
      },
    ];
  }
  // A plan that updates a branch is carried out to the end (merge, conflicts, commit, push), which the answer says.
  const sync = plan.actions.some((action) => action.kind === 'code.sync');
  const mr = plan.actions.some((action) => action.kind === 'gitlab.merge-request');
  const slite = plan.actions.some((action) => action.kind === 'slite.write');
  const asana = plan.actions.some((action) => action.kind === 'asana.create-task');
  const approval = asana ? 'planAsanaYes' : slite ? 'planSliteYes' : mr ? 'planMRYes' : sync ? 'planSyncYes' : 'planYes';
  return [
    {
      id: 'plan',
      text: t('chat.planQuestion', { goal: plan.goal }),
      options: [
        { id: 'approve', label: t(`chat.${approval}`), description: t(`chat.${approval}Help`) },
        { id: 'decline', label: t('chat.planNo'), description: null },
      ],
    },
  ];
}

/** The positions of the actions ticked on a picked plan, in the plan's order. */
export const pickedActions = (answers: QuestionAnswer[]): number[] =>
  (answers[0]?.optionIds ?? []).map((id) => Number(id.replace('action-', ''))).sort((a, b) => a - b);

/** Whether the single answer of a yes/no card says yes. */
export const approves = (answers: QuestionAnswer[]): boolean => answers[0]?.optionId === 'approve';

/** What waits for the user at the bottom of a conversation: its questions, then a plan, then a proposed change. */
export type Decision = 'questions' | 'plan' | 'proposal' | null;

export function pendingDecision(request: AssistantRequest): Decision {
  if (request.questions.length > 0) return 'questions';
  if (planQuestions(request).length > 0) return 'plan';
  if (request.proposal) return 'proposal';
  return null;
}
