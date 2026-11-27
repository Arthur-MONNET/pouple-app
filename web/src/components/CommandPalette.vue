<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue';
import { useRouter } from 'vue-router';
import { useAssistantStore } from '../composables/assistantStore';
import { useShortcuts } from '../composables/useShortcuts';
import { t } from '../i18n';
import { statusLabel } from '../lib/assistantRequests';
import { matchesCommand, recentConversations, searchConversations, stepIndex, type ConversationHit } from '../lib/search';
import { displayShortcut, isMac, type ShortcutId } from '../lib/shortcuts';
import StatusDot from './StatusDot.vue';

const emit = defineEmits<{ close: [] }>();
const store = useAssistantStore();
const router = useRouter();
const { shortcuts } = useShortcuts();

interface Command {
  id: string;
  label: string;
  keywords: string;
  hint: string;
  go: string;
}

const COMMANDS: readonly { id: string; route: string; shortcut?: ShortcutId }[] = [
  { id: 'new', route: 'new', shortcut: 'new-conversation' },
  { id: 'conversations', route: 'home' },
  { id: 'alerts', route: 'alerts' },
  { id: 'memory', route: 'memory' },
  { id: 'connectors', route: 'connectors' },
  { id: 'settings', route: 'settings' },
];

const query = ref('');
const active = ref(0);
const input = ref<HTMLInputElement | null>(null);
const listId = useId();
const mac = isMac();

const commands = computed<Command[]>(() =>
  COMMANDS.map((command) => ({
    id: command.id,
    label: t(`palette.commands.${command.id}`),
    keywords: t(`palette.keywords.${command.id}`),
    hint: command.shortcut ? displayShortcut(shortcuts.value[command.shortcut], mac) : '',
    go: command.route,
  })).filter((command) => matchesCommand(`${command.label} ${command.keywords}`, query.value)),
);
const hits = computed<ConversationHit[]>(() => (query.value.trim() === '' ? recentConversations(store.requests.value) : searchConversations(store.requests.value, query.value)));

type Row = { key: string; kind: 'conversation'; hit: ConversationHit } | { key: string; kind: 'command'; command: Command };
const rows = computed<Row[]>(() => [
  ...hits.value.map((hit): Row => ({ key: `c-${hit.request.id}`, kind: 'conversation', hit })),
  ...commands.value.map((command): Row => ({ key: `k-${command.id}`, kind: 'command', command })),
]);
const optionId = (index: number): string => `${listId}-${index}`;
const offsetOfCommands = computed(() => hits.value.length);

watch(query, () => (active.value = 0));
watch(active, async (index) => {
  await nextTick();
  document.getElementById(optionId(index))?.scrollIntoView({ block: 'nearest' });
});

function run(row: Row | undefined): void {
  if (!row) return;
  emit('close');
  void (row.kind === 'conversation' ? router.push({ name: 'conversation', params: { id: row.hit.request.id } }) : router.push({ name: row.command.go }));
}

function onKeydown(event: KeyboardEvent): void {
  const count = rows.value.length;
  if (event.key === 'Escape') {
    event.preventDefault();
    emit('close');
  } else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault();
    active.value = stepIndex(active.value, count, event.key === 'ArrowDown' ? 1 : -1);
  } else if (event.key === 'Enter') {
    event.preventDefault();
    run(rows.value[active.value]);
  } else if (event.key === 'Tab') {
    // The only thing to reach is the field: the results are chosen with the arrows.
    event.preventDefault();
  }
}

let previous: Element | null = null;
onMounted(async () => {
  previous = document.activeElement;
  await nextTick();
  input.value?.focus();
});
onBeforeUnmount(() => (previous instanceof HTMLElement ? previous.focus() : undefined));
</script>

<template>
  <Teleport to="body">
    <div class="fixed inset-0 z-50 flex items-start justify-center px-4 pt-[12vh]" @keydown="onKeydown">
      <div class="absolute inset-0 bg-black/60" aria-hidden="true" @click="emit('close')" />
      <div role="dialog" aria-modal="true" :aria-label="$t('palette.title')" class="relative flex max-h-[70vh] w-full max-w-xl flex-col overflow-hidden rounded-xl border border-line-strong bg-canvas shadow-2xl shadow-black/50" data-testid="palette">
        <input
          ref="input"
          v-model="query"
          type="text"
          role="combobox"
          aria-expanded="true"
          aria-autocomplete="list"
          :aria-controls="listId"
          :aria-activedescendant="rows.length > 0 ? optionId(active) : undefined"
          :placeholder="$t('palette.placeholder')"
          :aria-label="$t('palette.title')"
          autocomplete="off"
          spellcheck="false"
          class="h-12 w-full shrink-0 border-b border-line bg-transparent px-4 text-sm text-ink outline-none placeholder:text-ink-dim"
          data-testid="palette-input"
        />
        <div :id="listId" role="listbox" :aria-label="$t('palette.results')" class="min-h-0 flex-1 overflow-y-auto p-1.5">
          <div v-if="hits.length > 0" role="group" :aria-label="$t('palette.conversations')">
            <p class="px-2.5 pb-1 pt-1.5 text-2xs font-medium uppercase tracking-wider text-ink-dim" aria-hidden="true">{{ $t('palette.conversations') }}</p>
            <div
              v-for="(hit, index) in hits"
              :id="optionId(index)"
              :key="hit.request.id"
              role="option"
              :aria-selected="active === index"
              class="flex cursor-pointer items-start gap-2.5 rounded-lg px-2.5 py-2"
              :class="active === index ? 'bg-selected' : ''"
              data-testid="palette-conversation"
              @mousemove="active = index"
              @click="run(rows[index])"
            >
              <StatusDot :status="hit.request.status" class="mt-1.5" />
              <span class="min-w-0 flex-1">
                <span class="block truncate text-sm text-ink">{{ hit.title }}</span>
                <span v-if="hit.excerpt" class="block truncate text-xs text-ink-muted">{{ hit.excerpt.before }}<mark class="rounded bg-accent/25 px-0.5 text-ink">{{ hit.excerpt.match }}</mark>{{ hit.excerpt.after }}</span>
              </span>
              <span class="shrink-0 pt-0.5 text-2xs text-ink-dim">{{ hit.state ? $t(`palette.state.${hit.state}`) : statusLabel(hit.request.status) }}</span>
            </div>
          </div>
          <div v-if="commands.length > 0" role="group" :aria-label="$t('palette.commandsLabel')">
            <p class="px-2.5 pb-1 pt-2.5 text-2xs font-medium uppercase tracking-wider text-ink-dim" aria-hidden="true">{{ $t('palette.commandsLabel') }}</p>
            <div
              v-for="(command, index) in commands"
              :id="optionId(offsetOfCommands + index)"
              :key="command.id"
              role="option"
              :aria-selected="active === offsetOfCommands + index"
              class="flex cursor-pointer items-center gap-2.5 rounded-lg px-2.5 py-2"
              :class="active === offsetOfCommands + index ? 'bg-selected' : ''"
              data-testid="palette-command"
              @mousemove="active = offsetOfCommands + index"
              @click="run(rows[offsetOfCommands + index])"
            >
              <span class="min-w-0 flex-1 truncate text-sm text-ink">{{ command.label }}</span>
              <kbd v-if="command.hint" class="shrink-0 rounded border border-line px-1.5 py-0.5 font-sans text-2xs text-ink-muted">{{ command.hint }}</kbd>
            </div>
          </div>
          <p v-if="rows.length === 0" class="px-3 py-6 text-center text-xs text-ink-dim" data-testid="palette-empty">{{ $t('palette.empty', { query: query.trim() }) }}</p>
        </div>
        <p class="sr-only" role="status">{{ $t('palette.count', { count: rows.length }) }}</p>
      </div>
    </div>
  </Teleport>
</template>
