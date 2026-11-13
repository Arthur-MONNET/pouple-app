<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import type { AssistantQuestion, AssistantRequest, QuestionAnswer } from '../api/types';
import { errorMessage } from '../api/client';
import { useAssistantStore } from '../composables/assistantStore';
import { t } from '../i18n';
import { groupEntries, isWorking, requestTitle, statusLabel } from '../lib/assistantRequests';
import { formatReceived } from '../lib/format';
import AppButton from './AppButton.vue';
import AppIcon from './AppIcon.vue';
import AssistantQuestionCard from './AssistantQuestionCard.vue';
import AttachmentList from './AttachmentList.vue';
import ClampedBox from './ClampedBox.vue';
import MarkdownText from './MarkdownText.vue';
import InlineError from './InlineError.vue';
import MascotStage from './MascotStage.vue';

const props = defineProps<{ request: AssistantRequest }>();
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
const canElaborate = computed(() => (props.request.status === 'ready' || props.request.status === 'done') && props.request.continuable === true);
const groups = computed(() => groupEntries(props.request.entries));

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
const submitProposal = (answers: QuestionAnswer[]): Promise<void> =>
  guard(() => store.decide(props.request.id, answers[0]?.optionId === 'approve' ? 'approve' : 'decline'));

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
        <AppIcon v-if="request.source.permalink" name="external" size="sm" />
      </component>
      <hr class="flex-1 border-line-soft" />
    </div>

    <blockquote class="rounded-lg border-l-2 border-line-bright bg-surface px-3 py-2 text-xs text-ink-muted">
      <span class="whitespace-pre-wrap break-words">{{ request.text }}</span>
    </blockquote>

    <AttachmentList v-if="request.attachments && request.attachments.length > 0" :request-id="request.id" :attachments="request.attachments" />

    <template v-for="(group, index) in groups" :key="index">
      <ul v-if="group.type === 'steps'" class="flex flex-col gap-0.5 border-l border-line pl-2.5" :aria-label="$t('chat.didLabel')">
        <li
          v-for="entry in group.entries"
          :key="entry.id"
          class="line-clamp-3 break-all text-2xs leading-snug text-ink-dim"
          :class="entry.text.startsWith('$ ') ? 'font-mono' : ''"
          :title="entry.text"
        >
          {{ entry.text }}
        </li>
      </ul>
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
        <template v-else>
          <p class="whitespace-pre-wrap break-words">{{ group.entry.text }}</p>
          <AttachmentList v-if="group.entry.attachments && group.entry.attachments.length > 0" class="mt-2" :request-id="request.id" :attachments="group.entry.attachments" />
        </template>
      </div>
    </template>

    <div v-if="working" class="flex items-center gap-2 text-xs text-ink-dim">
      <p class="flex items-center gap-2" role="status">
        <span class="size-1.5 rounded-full bg-ok motion-safe:animate-pulse" aria-hidden="true" />
        {{ request.status === 'queued' ? statusLabel('queued') : $t('chat.working') }}
      </p>
      <AppButton variant="ghost" size="sm" class="ml-auto" :disabled="busy" :aria-label="$t('chat.stopAria')" @click="guard(() => store.stopRequest(request.id))">{{ $t('chat.stop') }}</AppButton>
    </div>

    <section v-if="request.draftReply && !ignored" class="rounded-lg border border-line bg-surface p-3">
      <div class="mb-1 flex items-center">
        <h3 class="text-xs font-medium text-ink-muted">{{ $t('chat.suggestedReply') }}</h3>
        <AppButton variant="ghost" size="sm" class="ml-auto" @click="copyReply">{{ copied ? $t('chat.copied') : $t('chat.copy') }}</AppButton>
      </div>
      <p class="whitespace-pre-wrap break-words text-ink">{{ request.draftReply }}</p>
    </section>

    <AssistantQuestionCard v-if="request.questions.length > 0" :title="title" :questions="request.questions" :busy="busy" @submit="submitAnswers" />
    <AssistantQuestionCard
      v-if="request.proposal"
      :title="title"
      :questions="proposalQuestion"
      :busy="busy"
      :allow-other="false"
      :submit-on-pick="false"
      submit-label="Confirm"
      @submit="submitProposal"
    />

    <div v-if="request.status === 'failed'" class="flex items-center gap-3 rounded-lg border border-line bg-surface p-2.5" data-testid="failed-note">
      <MascotStage pose="oops" />
      <InlineError>{{ request.error ?? $t('chat.failedFallback') }}</InlineError>
    </div>
    <p v-else-if="stoppedNote && !working" class="flex items-center gap-2 text-xs text-ink-muted" data-testid="stopped-note">
      <MascotStage pose="stop" size="sm" />
      {{ $t('chat.stopped') }}
    </p>
    <InlineError v-if="actionError">{{ actionError }}</InlineError>

    <div v-if="!working && request.status !== 'awaiting-user'" class="flex gap-1.5">
      <AppButton v-if="canElaborate" variant="ghost" size="sm" :disabled="busy" @click="guard(() => store.send(request.id, $t('chat.moreDetailMessage')))">{{ $t('chat.moreDetail') }}</AppButton>
      <AppButton variant="ghost" size="sm" :disabled="busy" @click="guard(() => store.retry(request.id))">{{ $t('chat.again') }}</AppButton>
      <AppButton v-if="request.status === 'ready' || request.status === 'done'" variant="ghost" size="sm" :disabled="busy" @click="guard(() => store.ignore(request.id))">{{ $t('chat.dismiss') }}</AppButton>
      <AppButton variant="ghost" size="sm" :disabled="busy" @click="remove">{{ $t('chat.delete') }}</AppButton>
    </div>
  </section>
</template>
