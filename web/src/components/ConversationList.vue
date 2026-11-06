<script setup lang="ts">
import { computed, ref } from 'vue';
import type { AssistantRequest } from '../api/types';
import { filterLabel, FILTERS, matchesFilter, requestTitle, statusLabel, type ConversationFilter } from '../lib/assistantRequests';
import { relativeTime } from '../lib/format';
import MascotStage from './MascotStage.vue';
import StatusDot from './StatusDot.vue';

const props = defineProps<{ requests: AssistantRequest[]; selectedId: string | null; attention: number; loaded: boolean }>();
defineEmits<{ select: [id: string] }>();

const filter = ref<ConversationFilter>('all');
const visible = computed(() => props.requests.filter((request) => matchesFilter(request, filter.value)));

/** Arrow keys move through the filters, as in any tab strip. */
function onFilterKey(event: KeyboardEvent): void {
  const step = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0;
  if (step === 0) return;
  const next = FILTERS[(FILTERS.indexOf(filter.value) + step + FILTERS.length) % FILTERS.length];
  if (next) filter.value = next;
  event.preventDefault();
}

const EMPTY: Record<ConversationFilter, string> = { all: 'list.emptyAll', 'needs-me': 'list.emptyNeedsMe', done: 'list.emptyDone' };
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="px-3 pb-2 pt-3">
      <div role="tablist" :aria-label="$t('filters.label')" class="flex rounded-lg bg-surface p-0.5" @keydown="onFilterKey">
        <button
          v-for="item in FILTERS"
          :key="item"
          type="button"
          role="tab"
          :aria-selected="filter === item"
          :tabindex="filter === item ? 0 : -1"
          class="flex-1 whitespace-nowrap rounded-md px-2 py-1 text-xs transition motion-reduce:transition-none"
          :class="filter === item ? 'bg-selected font-medium text-ink' : 'text-ink-muted hover:text-ink'"
          @click="filter = item"
        >
          {{ filterLabel(item) }}
          <span v-if="item === 'needs-me' && attention > 0" class="ml-1 rounded bg-accent/20 px-1 text-2xs text-accent">{{ attention }}</span>
        </button>
      </div>
    </div>

    <ul class="min-h-0 flex-1 overflow-y-auto px-1.5 pb-2" :aria-label="$t('list.label')">
      <li v-for="request in visible" :key="request.id">
        <button
          type="button"
          class="flex w-full items-start gap-2.5 rounded-lg px-2.5 py-2 text-left transition hover:bg-raised motion-reduce:transition-none"
          :class="request.id === selectedId ? 'bg-selected' : ''"
          :aria-current="request.id === selectedId ? 'true' : undefined"
          data-testid="conversation-item"
          @click="$emit('select', request.id)"
        >
          <StatusDot :status="request.status" class="mt-1.5" />
          <span class="min-w-0 flex-1">
            <span class="block truncate text-sm text-ink">{{ requestTitle(request) }}</span>
            <span class="block truncate text-xs" :class="request.id === selectedId ? 'text-ink-muted' : 'text-ink-dim'">{{ request.text }}</span>
            <span class="mt-0.5 block text-2xs" :class="request.id === selectedId ? 'text-ink-muted' : 'text-ink-dim'">{{ statusLabel(request.status) }}</span>
          </span>
          <time class="shrink-0 pt-0.5 text-2xs" :class="request.id === selectedId ? 'text-ink-muted' : 'text-ink-dim'" :datetime="request.updatedAt">{{ relativeTime(request.updatedAt) }}</time>
        </button>
      </li>
    </ul>
    <div v-if="loaded && visible.length === 0 && filter === 'all'" class="flex flex-col items-center gap-2 px-5 py-8 text-center" data-testid="empty-list">
      <MascotStage pose="sleep" size="md" class="!size-28" />
      <p class="font-display text-lg font-semibold text-ink">{{ $t('list.emptyTitle') }}</p>
      <p class="text-xs leading-relaxed text-ink-muted">{{ $t(EMPTY[filter]) }}</p>
    </div>
    <p v-else-if="loaded && visible.length === 0" class="px-4 py-6 text-center text-xs leading-relaxed text-ink-dim">{{ $t(EMPTY[filter]) }}</p>
    <p v-else-if="!loaded" class="px-4 py-6 text-center text-xs text-ink-dim" role="status">{{ $t('common.loading') }}</p>
  </div>
</template>
