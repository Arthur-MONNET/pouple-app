<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue';
import type { ReminderPick } from '../api/types';
import type { AssistantRequest } from '../api/types';
import { useAssistantStore } from '../composables/assistantStore';
import { useAsyncAction } from '../composables/useAsyncAction';
import { t } from '../i18n';
import { groupEntries, isWorking } from '../lib/assistantRequests';
import { sourceLabel } from '../lib/topicGroups';
import { providerName } from '../lib/usage';
import AppButton from './AppButton.vue';
import AppIcon from './AppIcon.vue';
import AssistantActivity from './AssistantActivity.vue';
import AttachmentList from './AttachmentList.vue';
import BrowserLive from './BrowserLive.vue';
import ClampedBox from './ClampedBox.vue';
import MarkdownText from './MarkdownText.vue';
import InlineError from './InlineError.vue';
import MascotStage from './MascotStage.vue';
import PlanSummary from './PlanSummary.vue';
import MemoryChangesCard from './MemoryChangesCard.vue';
import ReminderOfferCard from './ReminderOfferCard.vue';

/**
 * The thread of one conversation: the message it started from, what the assistant did and said, and what it offers
 * (a reply to copy, a plan's detail, reminders, memory changes). What waits for a decision is answered in the dock
 * below the thread, and what applies to the whole conversation is in its header.
 */
const props = defineProps<{ request: AssistantRequest; interactiveTerminal?: boolean }>();
const store = useAssistantStore();
const action = useAsyncAction();

/** The id of the text last copied, shown as copied for a moment. */
const copied = ref<string | null>(null);
let copiedTimer: ReturnType<typeof setTimeout> | undefined;
onBeforeUnmount(() => clearTimeout(copiedTimer));

const stoppedNote = computed(() => store.stopped.value.includes(props.request.id));
const working = computed(() => isWorking(props.request));
const ignored = computed(() => props.request.status === 'ignored');
/** More detail goes back to the AI in the same conversation: only a finished analysis has one to continue. */
const canElaborate = computed(() => (props.request.status === 'ready' || props.request.status === 'done') && !props.request.stopCheckpoint && props.request.continuable === true);
const groups = computed(() => {
  const all = groupEntries(props.request.entries);
  if (working.value && all.at(-1)?.type !== 'steps') all.push({ type: 'steps', entries: [] });
  return all;
});
/** A stable key per group: the id of its first entry, so a growing log keeps its open state. */
const groupKey = (group: (typeof groups.value)[number], index: number): string =>
  group.type === 'steps' ? `steps-${group.entries[0]?.id ?? index}` : group.entry.id;

/** A reminder is what the user asked to be reminded of: there is nothing to investigate again. */
const isReminder = computed(() => props.request.source.origin === 'reminder');
const originId = computed(() => props.request.source.relatedRequestId ?? null);
const remindersWaiting = computed(() => props.request.reminders?.status === 'proposed');
const remindersSet = computed(() => props.request.reminders?.status === 'accepted');
/** The last change the assistant made to the memory (the oldest come first), and how many came before it. */
const memoryChanges = computed(() => (props.request.memoryChanges ?? []).filter((change) => change.items.length > 0));
const lastMemoryChange = computed(() => memoryChanges.value[memoryChanges.value.length - 1] ?? null);

const confirmReminders = (items: ReminderPick[]): Promise<boolean> =>
  action.run(() => store.decideReminders(props.request.id, 'approve', props.request.reminders?.id ?? '', items));
const skipReminders = (): Promise<boolean> => action.run(() => store.decideReminders(props.request.id, 'decline', props.request.reminders?.id ?? ''));
const undoMemory = (changeId: string): Promise<boolean> => action.run(() => store.undoMemoryChange(props.request.id, changeId));
const shareMemory = (changeId: string, itemId: string, shared: boolean): Promise<boolean> =>
  action.run(() => store.shareMemoryFact(props.request.id, changeId, itemId, shared));

async function copy(id: string, text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text);
    copied.value = id;
    clearTimeout(copiedTimer);
    copiedTimer = setTimeout(() => (copied.value = null), 2000);
  } catch {
    action.error.value = t('chat.copyFailed');
  }
}

// An action error only describes the last attempt: once the request moves on, it no longer applies.
watch(
  () => props.request.status,
  () => action.clear(),
);
</script>

<template>
  <section class="flex flex-col gap-4" :class="{ 'opacity-60': ignored }" data-testid="assistant-request">
    <!-- The message the conversation started from: someone else's words, or the user's own note. -->
    <article class="rounded-xl border border-line bg-surface px-4 py-3" data-testid="source-message">
      <p class="mb-1 text-2xs font-medium uppercase tracking-wider text-ink-dim">{{ sourceLabel(request) }}</p>
      <p class="whitespace-pre-wrap break-words text-ink">{{ request.text }}</p>
      <AttachmentList v-if="request.attachments && request.attachments.length > 0" class="mt-2" :request-id="request.id" :attachments="request.attachments" />
    </article>

    <template v-for="(group, index) in groups" :key="groupKey(group, index)">
      <AssistantActivity
        v-if="group.type === 'steps'"
        :entries="group.entries"
        :working="working && index === groups.length - 1"
        :queued="request.status === 'queued'"
        :interactive="interactiveTerminal"
      />
      <div v-else-if="group.entry.role === 'user'" class="max-w-[85%] self-end rounded-2xl rounded-br-md bg-selected px-3.5 py-2 text-ink">
        <p class="whitespace-pre-wrap break-words">{{ group.entry.text }}</p>
        <AttachmentList v-if="group.entry.attachments && group.entry.attachments.length > 0" class="mt-2" :request-id="request.id" :attachments="group.entry.attachments" />
      </div>
      <InlineError v-else-if="group.entry.role === 'system'">{{ group.entry.text }}</InlineError>
      <div v-else class="group/answer flex flex-col gap-1.5 text-ink">
        <ClampedBox :length="group.entry.text.length">
          <MarkdownText :source="group.entry.text" />
        </ClampedBox>
        <!-- A picture the browser took comes with an assistant message: it shows under it like a file the user sent. -->
        <AttachmentList v-if="group.entry.attachments && group.entry.attachments.length > 0" :request-id="request.id" :attachments="group.entry.attachments" />
        <div class="flex opacity-0 transition group-hover/answer:opacity-100 group-focus-within/answer:opacity-100 motion-reduce:transition-none">
          <button
            type="button"
            class="inline-flex h-6 items-center gap-1 rounded px-1.5 text-2xs text-ink-dim transition hover:bg-raised hover:text-ink motion-reduce:transition-none"
            :aria-label="$t('chat.copyMessage')"
            @click="copy(group.entry.id, group.entry.text)"
          >
            <AppIcon :name="copied === group.entry.id ? 'check' : 'copy'" size="sm" />
            {{ copied === group.entry.id ? $t('chat.copied') : $t('chat.copy') }}
          </button>
        </div>
      </div>
    </template>

    <BrowserLive v-if="request.browser" :request-id="request.id" :browser="request.browser" />

    <section v-if="request.draftReply && !ignored" class="rounded-xl border border-accent/40 bg-surface p-3" data-testid="draft-reply">
      <div class="mb-1.5 flex items-center gap-2">
        <h3 class="text-xs font-medium text-ink-muted">{{ $t('chat.suggestedReply') }}</h3>
        <AppButton variant="primary" size="sm" class="ml-auto" @click="copy('draft', request.draftReply)">
          <AppIcon :name="copied === 'draft' ? 'check' : 'copy'" size="sm" />
          {{ copied === 'draft' ? $t('chat.copied') : $t('chat.copy') }}
        </AppButton>
      </div>
      <p class="whitespace-pre-wrap break-words text-ink">{{ request.draftReply }}</p>
    </section>

    <PlanSummary v-if="request.plan && request.plan.status === 'proposed'" :plan="request.plan" />

    <ReminderOfferCard
      v-if="request.reminders && remindersWaiting"
      :offer="request.reminders"
      :busy="action.busy.value"
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
      :busy="action.busy.value"
      @undo="undoMemory(lastMemoryChange.id)"
      @share="(itemId, shared) => shareMemory(lastMemoryChange?.id ?? '', itemId, shared)"
    />

    <div v-if="request.status === 'failed'" class="flex flex-wrap items-center gap-3 rounded-xl border border-fail/40 bg-surface p-3" data-testid="failed-note">
      <MascotStage pose="oops" />
      <InlineError class="min-w-0 flex-1">{{ request.error ?? $t('chat.failedFallback') }}</InlineError>
      <AppButton
        v-if="request.switchTo"
        variant="primary"
        size="sm"
        :disabled="action.busy.value"
        data-testid="continue-with"
        @click="action.run(() => store.continueWith(request.id, request.switchTo as string))"
      >
        {{ $t('chat.continueWith', { name: providerName(request.switchTo) }) }}
      </AppButton>
      <AppButton v-else-if="!isReminder" variant="outline" size="sm" :disabled="action.busy.value" @click="action.run(() => store.retry(request.id))">{{ $t('chat.again') }}</AppButton>
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
    <InlineError v-if="action.error.value">{{ action.error.value }}</InlineError>

    <div v-if="!working && request.status !== 'awaiting-user' && (canElaborate || (isReminder && originId && store.byId(originId)))" class="flex gap-1.5">
      <RouterLink
        v-if="isReminder && originId && store.byId(originId)"
        :to="{ name: 'conversation', params: { id: originId } }"
        class="inline-flex h-7 items-center rounded-full border border-line px-3 text-xs text-ink-muted transition hover:bg-raised hover:text-ink motion-reduce:transition-none"
      >
        {{ $t('chat.openOrigin') }}
      </RouterLink>
      <button
        v-if="canElaborate && !isReminder"
        type="button"
        class="inline-flex h-7 items-center rounded-full border border-line px-3 text-xs text-ink-muted transition hover:bg-raised hover:text-ink disabled:opacity-50 motion-reduce:transition-none"
        :disabled="action.busy.value"
        @click="action.run(() => store.send(request.id, $t('chat.moreDetailMessage')))"
      >
        {{ $t('chat.moreDetail') }}
      </button>
    </div>
  </section>
</template>
