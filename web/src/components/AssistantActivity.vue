<script setup lang="ts">
import { computed } from 'vue';
import type { ChatEntry } from '../api/types';
import { t } from '../i18n';
import AppIcon from './AppIcon.vue';

const props = defineProps<{
  entries: ChatEntry[];
  working: boolean;
  queued: boolean;
  interactive?: boolean;
}>();

function isCommand(entry: ChatEntry): boolean {
  return !!entry.command || entry.text.startsWith('$ ');
}

const finished = computed(() => props.entries.filter((entry) => {
  if (!isCommand(entry)) return false;
  return !entry.command || entry.command.status === 'completed' || entry.command.status === 'failed';
}).length);

const active = computed(() => props.entries.some((entry) => entry.command?.status === 'running'));
const hasCommands = computed(() => props.entries.some(isCommand));

function commandStatus(entry: ChatEntry): string {
  if (entry.command) return entry.command.status;
  if (entry.exitCode === undefined) return 'unknown';
  return entry.exitCode === 0 ? 'completed' : 'failed';
}

function commandLabel(entry: ChatEntry): string {
  const command = entry.command ? entry.text : entry.text.slice(2);
  if (/^git\s+(?:diff|log|merge-base|status|show)\b/.test(command)) return t('chat.activity.git');
  if (/^(?:go test|(?:npm|pnpm|yarn) (?:run )?test|pytest|vitest)\b/.test(command)) return t('chat.activity.tests');
  if (/^(?:rg|grep|find|fd)\b/.test(command)) return t('chat.activity.search');
  if (/^(?:cat|head|tail|nl|ls)\b/.test(command)) return t('chat.activity.read');
  return t('chat.activity.command');
}

const activityLabel = computed(() => {
  if (props.working && props.queued) return t('status.queued');
  if (props.working && props.interactive) return t('chat.activity.interactive');
  for (const entry of [...props.entries].reverse()) {
    if (/^(?:Edited|Wrote|Modifié|Écrit)/i.test(entry.text)) return t('chat.activity.edit');
    if (/^\{"(?:session|status)":/.test(entry.text)) return t('chat.activity.terminal');
  }
  return t(props.working ? 'chat.working' : 'chat.didLabel');
});
</script>

<template>
  <details class="min-w-0 rounded-md border border-line-soft bg-surface px-2.5 py-1.5" data-testid="activity-detail">
    <summary class="cursor-pointer text-xs text-ink-muted hover:text-ink">
      <span v-if="working || active" class="mx-1 inline-block size-1.5 rounded-full bg-accent motion-safe:animate-pulse" aria-hidden="true" />
      {{ hasCommands ? $t('chat.activity.finished', finished) : activityLabel }}
      <span v-if="active" class="ml-1 text-ink-dim" role="status">{{ $t('chat.activity.status.running') }}</span>
    </summary>
    <ol v-if="entries.length" class="mt-1.5 flex flex-col gap-0.5 border-l border-line pl-2.5" :aria-label="$t('chat.didLabel')">
      <li v-for="entry in entries" :key="entry.id" class="min-w-0">
        <details v-if="isCommand(entry)" class="group rounded border border-line-soft px-2 py-1">
          <summary class="flex cursor-pointer list-none items-center gap-1.5 text-2xs text-ink-muted hover:text-ink">
            <AppIcon name="chevron-right" size="sm" class="shrink-0 group-open:rotate-90" />
            <span>{{ commandLabel(entry) }}</span>
            <span class="ml-auto shrink-0" :class="commandStatus(entry) === 'failed' ? 'text-fail' : 'text-ink-dim'">
              {{ $t('chat.activity.status.' + commandStatus(entry)) }}
              <span v-if="entry.exitCode !== undefined"> · {{ $t('chat.activity.exitCode', { code: entry.exitCode }) }}</span>
            </span>
          </summary>
          <p v-if="!entry.command" class="mt-1.5 text-2xs text-ink-dim">{{ $t('chat.activity.excerpt') }}</p>
          <pre v-if="entry.command?.command || !entry.command" class="mt-1.5 max-h-32 overflow-auto rounded bg-raised p-2 font-mono text-2xs text-ink" tabindex="0">{{ entry.command ? entry.command.command : entry.text }}</pre>
          <pre v-if="entry.command?.output" class="mt-1.5 max-h-56 overflow-auto rounded bg-raised p-2 font-mono text-2xs text-ink-muted" tabindex="0" :aria-label="$t('chat.activity.output')">{{ entry.command.output }}</pre>
        </details>
        <p v-else class="line-clamp-3 break-all text-2xs leading-snug text-ink-dim" :title="entry.text">{{ entry.text }}</p>
      </li>
    </ol>
  </details>
</template>
