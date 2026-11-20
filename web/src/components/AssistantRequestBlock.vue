<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import type { AssistantQuestion, AssistantRequest, PlanAction, QuestionAnswer, ReminderPick } from '../api/types';
import { errorMessage } from '../api/client';
import { useAssistantStore } from '../composables/assistantStore';
import { t } from '../i18n';
import { groupEntries, isWorking, requestTitle } from '../lib/assistantRequests';
import { compactCount, formatReceived } from '../lib/format';
import { providerName, totalTokens } from '../lib/usage';
import AppButton from './AppButton.vue';
import AppIcon from './AppIcon.vue';
import AssistantActivity from './AssistantActivity.vue';
import AssistantQuestionCard from './AssistantQuestionCard.vue';
import AttachmentList from './AttachmentList.vue';
import BrowserLive from './BrowserLive.vue';
import ClampedBox from './ClampedBox.vue';
import MarkdownText from './MarkdownText.vue';
import InlineError from './InlineError.vue';
import MascotStage from './MascotStage.vue';
import PlanSummary from './PlanSummary.vue';
import MemoryChangesCard from './MemoryChangesCard.vue';
import ReminderOfferCard from './ReminderOfferCard.vue';

const props = defineProps<{ request: AssistantRequest; interactiveTerminal?: boolean }>();
const store = useAssistantStore();
const emit = defineEmits<{ removed: [] }>();

const actionError = ref<string | null>(null);
const busy = ref(false);
const copied = ref(false);

const stoppedNote = computed(() => store.stopped.value.includes(props.request.id));
const title = computed(() => requestTitle(props.request));
const working = computed(() => isWorking(props.request));
const ignored = computed(() => props.request.status === 'ignored');
/** More detail goes back to Codex in the same conversation: only a finished analysis has one to continue. */
const canElaborate = computed(() => (props.request.status === 'ready' || props.request.status === 'done') && !props.request.stopCheckpoint && props.request.continuable === true);
const groups = computed(() => {
  const all = groupEntries(props.request.entries);
  if (working.value && all.at(-1)?.type !== 'steps') all.push({ type: 'steps', entries: [] });
  return all;
});

/** A reminder is what the user asked to be reminded of: there is nothing to investigate again. */
const isReminder = computed(() => props.request.source.origin === 'reminder');
const originId = computed(() => props.request.source.relatedRequestId ?? null);
const remindersWaiting = computed(() => props.request.reminders?.status === 'proposed');
const remindersSet = computed(() => props.request.reminders?.status === 'accepted');
/** The last change the assistant made to the memory (the oldest come first), and how many came before it. */
const memoryChanges = computed(() => (props.request.memoryChanges ?? []).filter((change) => change.items.length > 0));
const lastMemoryChange = computed(() => memoryChanges.value[memoryChanges.value.length - 1] ?? null);

/** Approving a change is asked like any other question, so it looks and behaves the same. */
const proposalQuestion = computed<AssistantQuestion[]>(() => {
  const { proposal } = props.request;
  return proposal
    ? [
        {
          id: 'proposal',
          text: t('chat.proposalQuestion', { project: proposal.projectId, title: proposal.title }),
          options: [
            { id: 'approve', label: t('chat.proposalYes'), description: proposal.description },
            { id: 'decline', label: t('chat.proposalNo'), description: null },
          ],
        },
      ]
    : [];
});

/** What a plan's action is called where the user picks it: its kind, and the branches it names. */
function actionLabel(action: PlanAction): string {
  const branches = action.branch ? (action.base ? `${action.branch} ${action.kind === 'gitlab.merge-request' ? '→' : '←'} ${action.base}` : action.branch) : action.scope.id;
  return `${t(`chat.plan.kind.${action.kind}`)} · ${branches}`;
}

/**
 * Approving a plan is asked like any other question, so it looks and behaves the same. A plan with several actions lets
 * the user tick the ones they want: only those are carried out.
 */
const planQuestion = computed<AssistantQuestion[]>(() => {
  const { plan } = props.request;
  if (!plan || plan.status !== 'proposed') return [];
  if (plan.actions.length > 1) {
    return [
      {
        id: 'plan',
        text: t('chat.planPickQuestion', { goal: plan.goal }),
        multiple: true,
        options: plan.actions.map((action, index) => ({ id: `action-${index}`, label: actionLabel(action), description: action.summary })),
      },
    ];
  }
  // A plan that updates a branch is carried out to the end (merge, conflicts, commit, push), which the answer says.
  const sync = plan.actions.some((action) => action.kind === 'code.sync');
  const mr = plan.actions.some((action) => action.kind === 'gitlab.merge-request');
  return [
    {
      id: 'plan',
      text: t('chat.planQuestion', { goal: plan.goal }),
      options: [
        { id: 'approve', label: t(mr ? 'chat.planMRYes' : sync ? 'chat.planSyncYes' : 'chat.planYes'), description: t(mr ? 'chat.planMRYesHelp' : sync ? 'chat.planSyncYesHelp' : 'chat.planYesHelp') },
        { id: 'decline', label: t('chat.planNo'), description: null },
      ],
    },
  ];
});
const planIsPicked = computed(() => (props.request.plan?.actions.length ?? 0) > 1);

async function guard(action: () => Promise<unknown>): Promise<void> {
  busy.value = true;
  actionError.value = null;
  try {
    await action();
  } catch (cause) {
    actionError.value = errorMessage(cause);
  } finally {
    busy.value = false;
  }
}

const submitAnswers = (answers: QuestionAnswer[]): Promise<void> => guard(() => store.answer(props.request.id, answers));
const declinePlan = (): Promise<void> => guard(() => store.decidePlan(props.request.id, 'decline', props.request.plan?.id ?? ''));
const submitPlan = (answers: QuestionAnswer[]): Promise<void> => {
  const planId = props.request.plan?.id ?? '';
  if (planIsPicked.value) {
    const ticked = (answers[0]?.optionIds ?? []).map((id) => Number(id.replace('action-', ''))).sort((a, b) => a - b);
    return guard(() => store.decidePlan(props.request.id, 'approve', planId, ticked));
  }
  return guard(() => store.decidePlan(props.request.id, answers[0]?.optionId === 'approve' ? 'approve' : 'decline', planId));
};
const submitProposal = (answers: QuestionAnswer[]): Promise<void> =>
  guard(() => store.decide(props.request.id, answers[0]?.optionId === 'approve' ? 'approve' : 'decline'));

const confirmReminders = (items: ReminderPick[]): Promise<void> =>
  guard(() => store.decideReminders(props.request.id, 'approve', props.request.reminders?.id ?? '', items));
const undoMemory = (changeId: string): Promise<void> => guard(() => store.undoMemoryChange(props.request.id, changeId));
const shareMemory = (changeId: string, itemId: string, shared: boolean): Promise<void> =>
  guard(() => store.shareMemoryFact(props.request.id, changeId, itemId, shared));
const skipReminders = (): Promise<void> => guard(() => store.decideReminders(props.request.id, 'decline', props.request.reminders?.id ?? ''));

async function remove(): Promise<void> {
  await guard(() => store.remove(props.request.id));
  if (!actionError.value) emit('removed');
}

async function copyReply(): Promise<void> {
  if (!props.request.draftReply) return;
  try {
    await navigator.clipboard.writeText(props.request.draftReply);
    copied.value = true;
    setTimeout(() => (copied.value = false), 2000);
  } catch {
    actionError.value = t('chat.copyFailed');
  }
}

// An action error only describes the last attempt: once the request moves on, it no longer applies.
watch(
  () => props.request.status,
  () => (actionError.value = null),
);
</script>

<template>
  <section class="flex flex-col gap-3" :class="{ 'opacity-60': ignored }" :aria-label="title" data-testid="assistant-request">
    <div class="flex items-center gap-3 pt-2">
      <hr class="flex-1 border-line-soft" />
      <component
        :is="request.source.permalink ? 'a' : 'span'"
        v-bind="request.source.permalink ? { href: request.source.permalink, target: '_blank', rel: 'noopener noreferrer', title: t('chat.openInSlack') } : {}"
        class="flex max-w-[80%] items-center gap-1.5 truncate rounded-full border border-line px-2.5 py-1 text-2xs text-ink-muted"
        :class="{ 'hover:text-ink': request.source.permalink }"
      >
        <span class="truncate font-medium text-ink">{{ title }}</span>
        <span class="shrink-0 text-ink-dim">· {{ formatReceived(request.receivedAt) }}</span>
        <span v-if="request.tokens" class="shrink-0 text-ink-dim">· {{ $t('chat.tokens', { tokens: compactCount(totalTokens(request.tokens)) }) }}</span>
        <AppIcon v-if="request.source.permalink" name="external" size="sm" />
      </component>
      <hr class="flex-1 border-line-soft" />
    </div>

    <blockquote class="rounded-lg border-l-2 border-line-bright bg-surface px-3 py-2 text-xs text-ink-muted">
      <span class="whitespace-pre-wrap break-words">{{ request.text }}</span>
    </blockquote>

    <AttachmentList v-if="request.attachments && request.attachments.length > 0" :request-id="request.id" :attachments="request.attachments" />

    <template v-for="(group, index) in groups" :key="index">
      <AssistantActivity
        v-if="group.type === 'steps'"
        :entries="group.entries"
        :working="working && index === groups.length - 1"
        :queued="request.status === 'queued'"
        :interactive="interactiveTerminal"
      />
      <div
        v-else
        class="max-w-[92%] rounded-lg px-3 py-2"
        :class="{
          'self-end bg-selected text-ink': group.entry.role === 'user',
          'bg-raised text-ink': group.entry.role === 'assistant',
          'text-fail': group.entry.role === 'system',
        }"
      >
        <ClampedBox v-if="group.entry.role === 'assistant'" :length="group.entry.text.length">
          <MarkdownText :source="group.entry.text" />
        </ClampedBox>
        <p v-else class="whitespace-pre-wrap break-words">{{ group.entry.text }}</p>
        <!-- A picture the browser took comes with an assistant message: it shows under it like a file the user sent. -->
        <AttachmentList v-if="group.entry.attachments && group.entry.attachments.length > 0" class="mt-2" :request-id="request.id" :attachments="group.entry.attachments" />
      </div>
    </template>

    <BrowserLive v-if="request.browser" :request-id="request.id" :browser="request.browser" />

    <div v-if="working" class="flex items-center gap-2 text-xs text-ink-dim">
      <AppButton variant="ghost" size="sm" class="ml-auto" :disabled="busy" :aria-label="$t('chat.stopAria')" @click="guard(() => store.stopRequest(request.id))">{{ $t('chat.stop') }}</AppButton>
    </div>

    <section v-if="request.draftReply && !ignored" class="rounded-lg border border-line bg-surface p-3">
      <div class="mb-1 flex items-center">
        <h3 class="text-xs font-medium text-ink-muted">{{ $t('chat.suggestedReply') }}</h3>
        <AppButton variant="ghost" size="sm" class="ml-auto" @click="copyReply">{{ copied ? $t('chat.copied') : $t('chat.copy') }}</AppButton>
      </div>
      <p class="whitespace-pre-wrap break-words text-ink">{{ request.draftReply }}</p>
    </section>

    <AssistantQuestionCard
      v-if="request.questions.length > 0"
      :title="title"
      :questions="request.questions"
      :busy="busy"
      ignorable
      @submit="submitAnswers"
      @ignore="guard(() => store.ignore(request.id))"
    />
    <PlanSummary v-if="request.plan && request.plan.status === 'proposed'" :plan="request.plan" />
    <AssistantQuestionCard
      v-if="planQuestion.length > 0"
      :title="title"
      :questions="planQuestion"
      :busy="busy"
      :allow-other="false"
      :submit-on-pick="false"
      :ignorable="planIsPicked"
      :submit-label="$t('question.confirm')"
      @submit="submitPlan"
      @ignore="declinePlan"
    />
    <AssistantQuestionCard
      v-if="request.proposal"
      :title="title"
      :questions="proposalQuestion"
      :busy="busy"
      :allow-other="false"
      :submit-on-pick="false"
      :submit-label="$t('question.confirm')"
      @submit="submitProposal"
    />

    <ReminderOfferCard
      v-if="request.reminders && remindersWaiting"
      :title="title"
      :offer="request.reminders"
      :busy="busy"
      @confirm="confirmReminders"
      @skip="skipReminders"
    />
    <p v-else-if="remindersSet" class="flex items-center gap-2 text-xs text-ink-muted" data-testid="reminders-set">
      <AppIcon name="bell" size="sm" />
      {{ $t('chat.reminders.done') }}
      <RouterLink to="/alerts" class="underline decoration-line-bright underline-offset-2 hover:text-ink">{{ $t('chat.reminders.seeAlerts') }}</RouterLink>
    </p>

    <MemoryChangesCard
      v-if="lastMemoryChange"
      :change="lastMemoryChange"
      :earlier="memoryChanges.length - 1"
      :busy="busy"
      @undo="undoMemory(lastMemoryChange.id)"
      @share="(itemId, shared) => shareMemory(lastMemoryChange?.id ?? '', itemId, shared)"
    />

    <div v-if="request.status === 'failed'" class="flex flex-wrap items-center gap-3 rounded-lg border border-line bg-surface p-2.5" data-testid="failed-note">
      <MascotStage pose="oops" />
      <InlineError class="min-w-0 flex-1">{{ request.error ?? $t('chat.failedFallback') }}</InlineError>
      <AppButton
        v-if="request.switchTo"
        variant="primary"
        size="sm"
        :disabled="busy"
        data-testid="continue-with"
        @click="guard(() => store.continueWith(request.id, request.switchTo as string))"
      >
        {{ $t('chat.continueWith', { name: providerName(request.switchTo) }) }}
      </AppButton>
    </div>
    <details v-else-if="request.stopCheckpoint && !working" class="text-xs text-ink-muted">
      <summary class="cursor-pointer">{{ $t('chat.stopped') }}</summary>
      <p class="mt-1 whitespace-pre-line">{{ request.stopCheckpoint }}</p>
      <p>{{ $t('terminal.resumeReason') }}</p>
    </details>
    <p v-else-if="stoppedNote && !working" class="flex items-center gap-2 text-xs text-ink-muted" data-testid="stopped-note">
      <MascotStage pose="stop" size="sm" />
      {{ $t('chat.stopped') }}
    </p>
    <InlineError v-if="actionError">{{ actionError }}</InlineError>

    <div v-if="!working && request.status !== 'awaiting-user'" class="flex gap-1.5">
      <RouterLink v-if="isReminder && originId && store.byId(originId)" :to="{ name: 'conversation', params: { id: originId } }" class="inline-flex h-7 items-center rounded-md px-2.5 text-xs text-ink-muted transition hover:bg-raised hover:text-ink motion-reduce:transition-none">{{ $t('chat.openOrigin') }}</RouterLink>
      <AppButton v-if="canElaborate && !isReminder" variant="ghost" size="sm" :disabled="busy" @click="guard(() => store.send(request.id, $t('chat.moreDetailMessage')))">{{ $t('chat.moreDetail') }}</AppButton>
      <AppButton v-if="!isReminder" variant="ghost" size="sm" :disabled="busy" @click="guard(() => store.retry(request.id))">{{ $t(request.stopCheckpoint ? 'terminal.resume' : 'chat.again') }}</AppButton>
      <AppButton v-if="request.status === 'ready' || request.status === 'done'" variant="ghost" size="sm" :disabled="busy" @click="guard(() => store.ignore(request.id))">{{ $t('chat.dismiss') }}</AppButton>
      <AppButton variant="ghost" size="sm" :disabled="busy" @click="remove">{{ $t('chat.delete') }}</AppButton>
    </div>
  </section>
</template>
