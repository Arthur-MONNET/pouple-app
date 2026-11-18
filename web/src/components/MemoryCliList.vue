<script setup lang="ts">
import { useId } from 'vue';
import type { MemoryCli, MemoryCliId } from '../api/types';
import AppButton from './AppButton.vue';

/** The CLIs that can read the memory. Pouple writes the shown lines into the user's own file only on a click. */
defineProps<{ clis: readonly MemoryCli[]; enabled: boolean; busy: boolean }>();
defineEmits<{ toggle: [id: MemoryCliId, connected: boolean] }>();
const hintId = useId();
</script>

<template>
  <ul class="flex flex-col divide-y divide-line" :aria-label="$t('settings.memory.cli.listLabel')" data-testid="memory-clis">
    <li v-for="cli in clis" :key="cli.id" class="flex flex-col gap-2 py-4" data-testid="memory-cli">
      <div class="flex items-start justify-between gap-3">
        <div class="min-w-0">
          <p class="text-sm font-medium text-ink">
            {{ cli.name }}
            <span v-if="cli.connected" class="ml-1 rounded bg-accent/20 px-1 text-2xs font-normal text-accent">{{ $t('settings.memory.cli.connected') }}</span>
          </p>
          <p class="break-all font-mono text-xs text-ink-muted">{{ $t('settings.memory.cli.file', { file: cli.file }) }}</p>
        </div>
        <AppButton
          v-if="cli.connected"
          variant="outline"
          :disabled="busy"
          :aria-label="$t('settings.memory.cli.disconnectLabel', { name: cli.name })"
          data-testid="memory-cli-disconnect"
          @click="$emit('toggle', cli.id, false)"
        >
          {{ $t('settings.memory.cli.disconnect') }}
        </AppButton>
        <AppButton
          v-else
          variant="primary"
          :disabled="busy || !enabled"
          :aria-label="$t('settings.memory.cli.connectLabel', { name: cli.name })"
          :aria-describedby="enabled ? undefined : hintId"
          data-testid="memory-cli-connect"
          @click="$emit('toggle', cli.id, true)"
        >
          {{ $t('settings.memory.cli.connect') }}
        </AppButton>
      </div>
      <pre :aria-label="$t('settings.memory.cli.linesLabel', { name: cli.name })" class="overflow-x-auto whitespace-pre-wrap break-words rounded-lg border border-line bg-raised p-3 font-mono text-xs text-ink">{{ cli.lines }}</pre>
    </li>
  </ul>
  <p v-if="!enabled" :id="hintId" class="pb-2 text-xs text-ink-muted" data-testid="memory-cli-hint">{{ $t('settings.memory.cli.needsMemory') }}</p>
</template>
