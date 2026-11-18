<script setup lang="ts">
import type { MemoryChange } from '../api/types';
import { formatPlainDay } from '../lib/format';
import AppButton from './AppButton.vue';
import AppIcon from './AppIcon.vue';

const props = withDefaults(defineProps<{ change: MemoryChange; earlier?: number; busy?: boolean }>(), { earlier: 0, busy: false });
const emit = defineEmits<{ undo: []; share: [itemId: string, shared: boolean] }>();

/** Without a commit id the core cannot go back: there is nothing to undo. */
const canUndo = (): boolean => props.change.id !== '' && props.change.undone !== true && !props.busy;
</script>

<template>
  <section class="flex flex-col gap-2" data-testid="memory-changes">
    <div class="rounded-xl border border-line bg-surface p-3 transition motion-reduce:transition-none" :class="{ 'opacity-60': change.undone }">
      <div class="mb-2 flex items-center gap-2">
        <p class="flex items-center gap-1.5 font-medium text-ink">
          <AppIcon name="pencil" />
          {{ $t('chat.memoryChanges.title') }}
        </p>
        <span v-if="change.undone" class="rounded bg-raised px-1.5 py-0.5 text-xs text-ink-muted" data-testid="memory-undone">{{ $t('chat.memoryChanges.undone') }}</span>
        <AppButton variant="outline" size="sm" class="ml-auto" :disabled="!canUndo()" data-testid="memory-undo" @click="emit('undo')">{{ $t('chat.memoryChanges.undo') }}</AppButton>
      </div>
      <ul class="flex flex-col gap-1.5" :aria-label="$t('chat.memoryChanges.listLabel')">
        <li v-for="item in change.items" :key="item.id" class="flex flex-wrap items-center gap-2 rounded-lg border border-line px-2.5 py-2" data-testid="memory-change-item">
          <div class="min-w-0 flex-1">
            <p class="flex flex-wrap items-center gap-1.5 text-xs text-ink-muted">
              <span class="rounded bg-raised px-1.5 py-0.5 text-ink" data-testid="memory-kind">{{ $t(`chat.memoryChanges.kind.${item.kind}`) }}</span>
              <span class="min-w-0 break-words">{{ item.noteTitle }}</span>
              <span v-if="item.unconfirmed" class="rounded border border-line-strong px-1.5 py-0.5 text-ink-muted" data-testid="memory-unconfirmed">{{ $t('chat.memoryChanges.unconfirmed') }}</span>
              <span v-if="item.review" class="text-ink-dim" data-testid="memory-review-date">{{ $t('chat.memoryChanges.review', { date: formatPlainDay(item.review) }) }}</span>
            </p>
            <p class="mt-0.5 break-words text-ink" :class="{ 'line-through decoration-ink-dim': item.kind === 'remove' }">{{ item.fact }}</p>
            <p v-if="item.kind === 'update' && item.previous" class="break-words text-xs text-ink-muted" data-testid="memory-previous">{{ $t('chat.memoryChanges.previous', { fact: item.previous }) }}</p>
          </div>
          <button
            v-if="item.kind !== 'remove'"
            type="button"
            role="switch"
            :aria-checked="item.shared"
            :aria-label="$t('chat.memoryChanges.sharedLabel', { fact: item.fact })"
            :disabled="busy || change.undone"
            class="h-8 rounded-md border px-2.5 text-xs transition disabled:cursor-not-allowed disabled:opacity-50 motion-reduce:transition-none"
            :class="{ 'border-accent bg-accent text-canvas': item.shared, 'border-line-strong text-ink-muted': !item.shared }"
            data-testid="memory-shared"
            @click="emit('share', item.id, !item.shared)"
          >
            {{ item.shared ? $t('chat.memoryChanges.shared') : $t('chat.memoryChanges.private') }}
          </button>
        </li>
      </ul>
      <p class="mt-2 text-xs text-ink-dim">{{ $t('chat.memoryChanges.privacy') }}</p>
    </div>
    <p v-if="earlier > 0" class="text-xs text-ink-muted" data-testid="memory-earlier">{{ $t('chat.memoryChanges.earlier', { n: earlier }, earlier) }}</p>
  </section>
</template>
