<script setup lang="ts">
import { computed } from 'vue';
import type { AssistantRequest } from '../api/types';
import { useAssistantStore } from '../composables/assistantStore';
import { useAsyncAction } from '../composables/useAsyncAction';
import { t } from '../i18n';
import { isWorking, permalinkLabelKey, requestTitle, statusLabel } from '../lib/assistantRequests';
import { compactCount, formatReceived } from '../lib/format';
import { totalTokens } from '../lib/usage';
import ActionMenu, { type MenuItem } from './ActionMenu.vue';
import AppIcon from './AppIcon.vue';
import InlineError from './InlineError.vue';
import StatusDot from './StatusDot.vue';

const props = defineProps<{ request: AssistantRequest }>();
const emit = defineEmits<{ removed: [id: string] }>();
const store = useAssistantStore();
const action = useAsyncAction();

const working = computed(() => isWorking(props.request));
const isReminder = computed(() => props.request.source.origin === 'reminder');

/** What can be done to the whole conversation; deleting comes last and apart. */
const items = computed<MenuItem[]>(() => [
  ...(!working.value && !isReminder.value && props.request.status !== 'awaiting-user'
    ? [{ id: 'retry', label: t(props.request.stopCheckpoint ? 'terminal.resume' : 'chat.again') }]
    : []),
  ...(props.request.status === 'ready' || props.request.status === 'done' ? [{ id: 'dismiss', label: t('chat.dismiss') }] : []),
  { id: 'delete', label: t('chat.delete'), danger: true },
]);

async function onSelect(id: string): Promise<void> {
  if (id === 'retry') await action.run(() => store.retry(props.request.id));
  else if (id === 'dismiss') await action.run(() => store.ignore(props.request.id));
  else if (id === 'delete') {
    // The conversation deleted, not the one shown when the deletion ends: the user may have opened another one meanwhile.
    const removed = props.request.id;
    if (await action.run(() => store.remove(removed))) emit('removed', removed);
  }
}
</script>

<template>
  <header class="shrink-0 border-b border-line-soft px-4 py-2.5" data-testid="conversation-header">
    <div class="mx-auto flex w-full max-w-3xl items-center gap-3">
      <StatusDot :status="request.status" />
      <div class="min-w-0 flex-1">
        <h1 class="truncate text-sm font-semibold text-ink">{{ requestTitle(request) }}</h1>
        <p class="flex items-center gap-1.5 truncate text-2xs text-ink-dim">
          <span>{{ statusLabel(request.status) }}</span>
          <span aria-hidden="true">·</span>
          <time :datetime="request.receivedAt">{{ formatReceived(request.receivedAt) }}</time>
          <template v-if="request.tokens">
            <span aria-hidden="true">·</span>
            <span>{{ $t('chat.tokens', { tokens: compactCount(totalTokens(request.tokens)) }) }}</span>
          </template>
        </p>
      </div>
      <a
        v-if="request.source.permalink"
        :href="request.source.permalink"
        target="_blank"
        rel="noopener noreferrer"
        class="inline-flex h-8 items-center gap-1.5 rounded-md px-2.5 text-xs text-ink-muted transition hover:bg-raised hover:text-ink motion-reduce:transition-none"
      >
        <AppIcon name="external" size="sm" />
        {{ $t(permalinkLabelKey(request.source.origin)) }}
      </a>
      <ActionMenu :label="$t('chat.actionsMenu')" :items="items" :disabled="action.busy.value" @select="onSelect" />
    </div>
    <InlineError v-if="action.error.value" class="mx-auto mt-1.5 max-w-3xl">{{ action.error.value }}</InlineError>
  </header>
</template>
