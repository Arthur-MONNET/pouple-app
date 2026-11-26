<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import type { AssistantRequest, AssistantTopic } from '../api/types';
import { filterLabel, FILTERS, inView, isInApp, statusLabel, type ConversationFilter } from '../lib/assistantRequests';
import { isArchived, isAway, isSnoozed } from '../lib/shelf';
import { groupByTopic, previewOf, sourceLabel, type ConversationGroup } from '../lib/topicGroups';
import { shownRequests } from '../lib/topicFolds';
import { relativeTime, untilTime } from '../lib/format';
import { t } from '../i18n';
import { useSpringLoad } from '../composables/useSpringLoad';
import { useTopicFolds } from '../composables/useTopicFolds';
import AppIcon from './AppIcon.vue';
import MascotStage from './MascotStage.vue';
import StatusDot from './StatusDot.vue';

const props = defineProps<{ requests: AssistantRequest[]; topics?: AssistantTopic[]; selectedId: string | null; attention: number; loaded: boolean }>();
const emit = defineEmits<{ select: [id: string] }>();

const filter = ref<ConversationFilter>('all');
// The archive view shows what the user put away or postponed, and only that; a filter button leaves it.
const archive = ref(false);
const away = computed(() => props.requests.filter(isAway).length);
// Leaving the last archived conversation (or undoing its archive) returns to the main list instead of an empty view.
watch(away, (count) => {
  if (count === 0) archive.value = false;
});
const visible = computed(() => props.requests.filter((request) => inView(request, filter.value, archive.value)));
const groups = computed(() => groupByTopic(visible.value, props.topics ?? []));
const pick = (item: ConversationFilter): void => {
  filter.value = item;
  archive.value = false;
};

// Which topics are open follows the whole main list, not the filter, so picking a filter does not move the defaults.
const allGroups = computed(() => groupByTopic(props.requests.filter((request) => !isAway(request)), props.topics ?? []));
/** What a row says under its title: the status, or why it is out of the main list. */
const stateLabel = (request: AssistantRequest): string =>
  isSnoozed(request) ? t('shelf.snoozed', { time: untilTime(request.snoozedUntil ?? '') }) : isArchived(request) ? t('shelf.archivedLabel') : statusLabel(request.status);
const folds = useTopicFolds(allGroups, () => props.selectedId);
const { isOpen, toggle } = folds;
const groupKeys = computed(() => groups.value.map((group) => group.key));
const anyOpen = computed(() => folds.anyOpen(groupKeys.value));
const toggleAll = (): void => folds.setAll(groupKeys.value, !anyOpen.value);

// Topics showing all of their conversations. Not kept: a reload starts again from the first few.
const expanded = ref<ReadonlySet<string>>(new Set());
function toggleExpanded(key: string): void {
  const next = new Set(expanded.value);
  if (next.has(key)) next.delete(key);
  else next.add(key);
  expanded.value = next;
}
// A folded topic still shows the open conversation, so a selection is never hidden.
const shownIn = (group: ConversationGroup): { shown: AssistantRequest[]; hidden: number } =>
  shownRequests(group.requests, isOpen(group.key), expanded.value.has(group.key), props.selectedId);

// A file dragged over a conversation, or over a folded group, for a moment opens it, so it can be dropped inside.
const CONVERSATION = 'conversation:';
const GROUP = 'group:';
const springLoad = useSpringLoad(
  (key) => (key.startsWith(GROUP) ? toggle(key.slice(GROUP.length)) : emit('select', key.slice(CONVERSATION.length))),
  (key) => (key.startsWith(GROUP) ? !isOpen(key.slice(GROUP.length)) : key.slice(CONVERSATION.length) !== props.selectedId),
  () => window.matchMedia('(prefers-reduced-motion: reduce)').matches,
);
const blinking = (key: string): boolean => springLoad.target.value === key && springLoad.lit.value;

// A drag that ends anywhere, dropped or not, ends the wait.
onMounted(() => {
  window.addEventListener('drop', springLoad.reset);
  window.addEventListener('dragend', springLoad.reset);
});
onBeforeUnmount(() => {
  window.removeEventListener('drop', springLoad.reset);
  window.removeEventListener('dragend', springLoad.reset);
});

const EMPTY: Record<ConversationFilter, string> = { all: 'list.emptyAll', 'needs-me': 'list.emptyNeedsMe', done: 'list.emptyDone' };
const emptyKey = computed(() => (archive.value ? 'list.emptyArchive' : EMPTY[filter.value]));
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="flex items-center gap-1.5 px-3 pb-2 pt-3">
      <!-- A row of toggle buttons, not tabs: the list below stays one list whatever the filter. -->
      <div role="group" :aria-label="$t('filters.label')" class="flex min-w-0 flex-1 rounded-lg bg-surface p-0.5">
        <button
          v-for="item in FILTERS"
          :key="item"
          type="button"
          :aria-pressed="!archive && filter === item"
          class="flex-1 whitespace-nowrap rounded-md px-2 py-1 text-xs transition motion-reduce:transition-none"
          :class="{ 'bg-selected font-medium text-ink': !archive && filter === item, 'text-ink-muted hover:text-ink': archive || filter !== item }"
          @click="pick(item)"
        >
          {{ filterLabel(item) }}
          <span v-if="item === 'needs-me' && attention > 0" class="ml-1 rounded bg-accent/20 px-1 text-2xs text-accent">{{ attention }}</span>
        </button>
      </div>
      <button
        v-if="groups.length > 0"
        type="button"
        class="inline-flex size-7 shrink-0 items-center justify-center rounded-md text-ink-muted transition hover:bg-raised hover:text-ink motion-reduce:transition-none"
        :aria-label="anyOpen ? $t('list.collapseAll') : $t('list.expandAll')"
        :title="anyOpen ? $t('list.collapseAll') : $t('list.expandAll')"
        data-testid="toggle-all-topics"
        @click="toggleAll"
      >
        <svg class="size-4" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <template v-if="anyOpen"><path d="M5 2.5 8 5.5l3-3" /><path d="M5 13.5l3-3 3 3" /></template>
          <template v-else><path d="M5 5.5l3-3 3 3" /><path d="M5 10.5l3 3 3-3" /></template>
        </svg>
      </button>
    </div>

    <ul class="min-h-0 flex-1 overflow-y-auto px-1.5 pb-2" :aria-label="$t('list.label')">
      <li v-for="group in groups" :key="group.key" class="mb-1" data-testid="conversation-group">
        <button
          type="button"
          class="flex w-full items-center gap-1.5 rounded-md px-2 py-1.5 text-left text-xs font-medium transition hover:text-ink motion-reduce:transition-none"
          :class="{ 'bg-accent/25 text-ink': blinking(GROUP + group.key), 'text-ink-muted': !blinking(GROUP + group.key) }"
          :aria-expanded="isOpen(group.key)"
          :aria-controls="`group-${group.key}`"
          :aria-label="$t('groups.toggle', { name: group.label })"
          @click="toggle(group.key)"
          @dragenter.prevent="springLoad.enter(GROUP + group.key, $event)"
          @dragover.prevent
          @dragleave="springLoad.leave(GROUP + group.key, $event)"
        >
          <svg class="size-3 shrink-0 transition-transform motion-reduce:transition-none" :class="isOpen(group.key) ? 'rotate-90' : ''" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M4.5 2.5 8 6l-3.5 3.5" /></svg>
          <span class="min-w-0 flex-1 truncate">{{ group.label }}</span>
          <span v-if="group.attention > 0" class="rounded bg-accent/20 px-1 text-2xs text-accent">{{ group.attention }}</span>
          <span class="text-2xs text-ink-dim">{{ group.requests.length }}</span>
        </button>
        <ul :id="`group-${group.key}`">
          <li v-for="request in shownIn(group).shown" :key="request.id">
            <button
              type="button"
              class="flex w-full items-start gap-2.5 rounded-lg px-2.5 py-2 text-left transition hover:bg-raised motion-reduce:transition-none"
              :class="{ 'bg-selected': request.id === selectedId && !blinking(CONVERSATION + request.id), 'bg-accent/25': blinking(CONVERSATION + request.id) }"
              :aria-current="request.id === selectedId ? 'true' : undefined"
              data-testid="conversation-item"
              @click="emit('select', request.id)"
              @dragenter.prevent="springLoad.enter(CONVERSATION + request.id, $event)"
              @dragover.prevent
              @dragleave="springLoad.leave(CONVERSATION + request.id, $event)"
            >
              <StatusDot :status="request.status" class="mt-1.5" />
              <span class="min-w-0 flex-1">
                <span class="flex min-w-0 items-center gap-1 text-sm text-ink">
                  <AppIcon v-if="isInApp(request)" name="pencil" size="sm" class="text-accent" data-testid="in-app-icon" />
                  <span class="truncate">{{ sourceLabel(request) }}</span>
                  <AppIcon v-if="request.pinnedAt" name="pin" size="sm" class="text-ink-dim" data-testid="pinned-icon" />
                </span>
                <span class="block truncate text-xs" :class="request.id === selectedId ? 'text-ink-muted' : 'text-ink-dim'">{{ previewOf(request) }}</span>
                <span class="mt-0.5 block text-2xs" :class="request.id === selectedId ? 'text-ink-muted' : 'text-ink-dim'">{{ stateLabel(request) }}</span>
              </span>
              <time class="shrink-0 pt-0.5 text-2xs" :class="request.id === selectedId ? 'text-ink-muted' : 'text-ink-dim'" :datetime="request.updatedAt">{{ relativeTime(request.updatedAt) }}</time>
            </button>
          </li>
          <li v-if="isOpen(group.key) && (shownIn(group).hidden > 0 || expanded.has(group.key))">
            <button
              type="button"
              class="w-full rounded-md px-2.5 py-1.5 text-left text-xs text-ink-muted transition hover:text-ink motion-reduce:transition-none"
              data-testid="topic-show-more"
              @click="toggleExpanded(group.key)"
            >
              {{ expanded.has(group.key) ? $t('list.showFewer') : $t('list.showMore', { count: shownIn(group).hidden }) }}
            </button>
          </li>
        </ul>
      </li>
    </ul>
    <button
      v-if="away > 0 || archive"
      type="button"
      class="mx-1.5 mb-1.5 flex items-center gap-2 rounded-md px-2.5 py-1.5 text-left text-xs transition hover:bg-raised hover:text-ink motion-reduce:transition-none"
      :class="archive ? 'bg-selected text-ink' : 'text-ink-muted'"
      :aria-pressed="archive"
      data-testid="toggle-archive"
      @click="archive = !archive"
    >
      <AppIcon name="archive" size="sm" />
      <span class="min-w-0 flex-1 truncate">{{ archive ? $t('shelf.backToList') : $t('shelf.archiveView', { count: away }) }}</span>
    </button>
    <div v-if="loaded && visible.length === 0 && filter === 'all'" class="flex flex-col items-center gap-2 px-5 py-8 text-center" data-testid="empty-list">
      <MascotStage pose="sleep" size="md" class="!size-28" />
      <p class="font-display text-lg font-semibold text-ink">{{ $t('list.emptyTitle') }}</p>
      <p class="text-xs leading-relaxed text-ink-muted">{{ $t(emptyKey) }}</p>
    </div>
    <p v-else-if="loaded && visible.length === 0" class="px-4 py-6 text-center text-xs leading-relaxed text-ink-dim">{{ $t(emptyKey) }}</p>
    <p v-else-if="!loaded" class="px-4 py-6 text-center text-xs text-ink-dim" role="status">{{ $t('common.loading') }}</p>
  </div>
</template>
