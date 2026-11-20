<script setup lang="ts">
import { computed } from 'vue';
import type { AssistantRequest, QuestionAnswer } from '../api/types';
import { useAssistantStore } from '../composables/assistantStore';
import { useAsyncAction } from '../composables/useAsyncAction';
import { t } from '../i18n';
import { approves, pendingDecision, pickedActions, planIsPicked, planQuestions, proposalQuestions } from '../lib/requestDecisions';
import AssistantQuestionCard from './AssistantQuestionCard.vue';
import InlineError from './InlineError.vue';

/**
 * What the assistant waits for, where the message field would be: its questions, a plan to approve or a change to
 * apply. Answering it is the only way on, so it takes the field's place instead of sitting above a field that cannot
 * be used.
 */
const props = defineProps<{ request: AssistantRequest }>();
const store = useAssistantStore();
const action = useAsyncAction();

const decision = computed(() => pendingDecision(props.request));
const picked = computed(() => planIsPicked(props.request));
const planId = computed(() => props.request.plan?.id ?? '');

const answer = (answers: QuestionAnswer[]): Promise<boolean> => action.run(() => store.answer(props.request.id, answers));
const ignore = (): Promise<boolean> => action.run(() => store.ignore(props.request.id));
const declinePlan = (): Promise<boolean> => action.run(() => store.decidePlan(props.request.id, 'decline', planId.value));
const submitPlan = (answers: QuestionAnswer[]): Promise<boolean> =>
  picked.value
    ? action.run(() => store.decidePlan(props.request.id, 'approve', planId.value, pickedActions(answers)))
    : action.run(() => store.decidePlan(props.request.id, approves(answers) ? 'approve' : 'decline', planId.value));
const submitProposal = (answers: QuestionAnswer[]): Promise<boolean> =>
  action.run(() => store.decide(props.request.id, approves(answers) ? 'approve' : 'decline'));
</script>

<template>
  <div v-if="decision" class="max-h-[55vh] overflow-y-auto px-4 py-3" data-testid="decision-dock">
    <InlineError v-if="action.error.value" class="mb-2">{{ action.error.value }}</InlineError>
    <AssistantQuestionCard
      v-if="decision === 'questions'"
      :questions="request.questions"
      :busy="action.busy.value"
      ignorable
      @submit="answer"
      @ignore="ignore"
    />
    <AssistantQuestionCard
      v-else-if="decision === 'plan'"
      :questions="planQuestions(request)"
      :busy="action.busy.value"
      :allow-other="false"
      :submit-on-pick="false"
      :ignorable="picked"
      :submit-label="t('question.confirm')"
      @submit="submitPlan"
      @ignore="declinePlan"
    />
    <AssistantQuestionCard
      v-else
      :questions="proposalQuestions(request)"
      :busy="action.busy.value"
      :allow-other="false"
      :submit-on-pick="false"
      :submit-label="t('question.confirm')"
      @submit="submitProposal"
    />
  </div>
</template>
