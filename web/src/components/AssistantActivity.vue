<script setup lang="ts">
import { computed } from 'vue';
import type { ChatEntry } from '../api/types';
import { t } from '../i18n';
import AppIcon from './AppIcon.vue';
import AppSpinner from './AppSpinner.vue';

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

/** The command itself, as it was run: what the log shows first. */
function commandText(entry: ChatEntry): string {
  if (entry.command?.command) return entry.command.command;
  return entry.command ? entry.text : entry.text.slice(2);
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
    if (/^\{"(?:session|status)":/.test(entry.text)) {
      return t(/"status":"(?:unavailable|start_failed)"/.test(entry.text) ? 'chat.activity.terminalRejected' : 'chat.activity.terminal');
    }
  }
  return t(props.working ? 'chat.working' : 'chat.didLabel');
});
</script>

<template>
  <details class="group/log min-w-0 rounded-lg border border-line-soft bg-surface/60" data-testid="activity-detail" :open="working || undefined">
    <summary class="flex cursor-pointer list-none items-center gap-2 px-3 py-2 text-xs text-ink-muted hover:text-ink">
      <AppIcon name="chevron-right" size="sm" class="shrink-0 transition-transform group-open/log:rotate-90 motion-reduce:transition-none" />
      <span v-if="working || active" class="inline-block size-1.5 shrink-0 rounded-full bg-lavender motion-safe:animate-pulse" aria-hidden="true" />
      <span class="min-w-0 flex-1 truncate">{{ hasCommands && !working ? $t('chat.activity.finished', finished) : activityLabel }}</span>
      <span v-if="active" class="shrink-0 text-ink-dim" role="status">{{ $t('chat.activity.status.running') }}</span>
    </summary>
    <ol v-if="entries.length" class="flex flex-col gap-0.5 border-t border-line-soft px-2 py-1.5" :aria-label="$t('chat.didLabel')">
      <li v-for="entry in entries" :key="entry.id" class="min-w-0">
        <details v-if="isCommand(entry)" class="group/cmd rounded-md">
          <summary class="flex cursor-pointer list-none items-center gap-2 rounded-md px-1.5 py-1 text-xs hover:bg-raised" :title="commandLabel(entry)">
            <AppSpinner v-if="commandStatus(entry) === 'running'" class="size-3.5 shrink-0 text-lavender" />
            <AppIcon v-else-if="commandStatus(entry) === 'failed' || commandStatus(entry) === 'rejected'" name="x-circle" size="sm" class="text-fail" />
            <AppIcon v-else-if="commandStatus(entry) === 'completed'" name="check-circle" size="sm" class="text-ok" />
            <AppIcon v-else name="terminal" size="sm" class="text-ink-dim" />
            <code class="min-w-0 flex-1 truncate font-mono text-2xs text-ink">{{ commandText(entry) }}</code>
            <span v-if="commandStatus(entry) !== 'unknown' || entry.exitCode !== undefined" class="shrink-0 text-2xs" :class="{ 'text-fail': commandStatus(entry) === 'failed', 'text-ink-dim': commandStatus(entry) !== 'failed' }">
              {{ $t('chat.activity.status.' + commandStatus(entry)) }}<template v-if="entry.exitCode !== undefined"> · {{ $t('chat.activity.exitCode', { code: entry.exitCode }) }}</template>
            </span>
          </summary>
          <div class="ml-6 mt-1 flex flex-col gap-1.5 pb-1.5">
            <p v-if="!entry.command" class="text-2xs text-ink-dim">{{ $t('chat.activity.excerpt') }}</p>
            <!-- The line above is cut when long: the whole command is shown once it is opened. -->
            <pre v-if="commandText(entry).length > 70" class="max-h-32 overflow-auto whitespace-pre-wrap break-all rounded bg-raised p-2 font-mono text-2xs text-ink" tabindex="0">{{ commandText(entry) }}</pre>
            <pre v-if="entry.command?.output" class="max-h-56 overflow-auto rounded bg-raised p-2 font-mono text-2xs text-ink-muted" tabindex="0" :aria-label="$t('chat.activity.output')">{{ entry.command.output }}</pre>
          </div>
        </details>
        <p v-else class="line-clamp-3 break-all px-1.5 py-0.5 text-2xs leading-snug text-ink-dim" :title="entry.text">{{ entry.text }}</p>
      </li>
    </ol>
  </details>
</template>
