<script setup lang="ts">
import { nextTick, ref, watch } from 'vue';
import AppIcon from './AppIcon.vue';

const props = defineProps<{ lines: string[]; running: boolean }>();
const open = ref(false);
const box = ref<HTMLElement | null>(null);

watch(
  () => props.lines.length,
  async () => {
    await nextTick();
    box.value?.scrollTo({ top: box.value.scrollHeight });
  },
);
// What is happening is worth seeing when it starts.
watch(
  () => props.running,
  (running) => running && (open.value = true),
);
</script>

<template>
  <div v-if="lines.length > 0" class="rounded-lg border border-line bg-surface">
    <button
      type="button"
      class="flex w-full items-center gap-2 px-3 py-2 text-left text-xs text-ink-muted hover:text-ink"
      :aria-expanded="open"
      aria-controls="action-log"
      @click="open = !open"
    >
      <AppIcon name="terminal" size="sm" />
      <span>Output</span>
      <span class="text-ink-dim">{{ lines.length }} {{ lines.length === 1 ? 'line' : 'lines' }}</span>
      <AppIcon :name="open ? 'chevron-down' : 'chevron-right'" size="sm" class="ml-auto" />
    </button>
    <div v-show="open" id="action-log" ref="box" class="max-h-40 overflow-y-auto border-t border-line-soft px-3 py-2" tabindex="0" aria-label="Output of the action">
      <p v-for="(line, index) in lines" :key="index" class="break-all font-mono text-2xs leading-relaxed text-ink-muted">{{ line }}</p>
    </div>
  </div>
</template>
