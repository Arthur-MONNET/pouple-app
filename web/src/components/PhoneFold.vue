<script setup lang="ts">
import AppIcon from './AppIcon.vue';

/** A collapsed row that opens onto the technical settings it names. */
defineProps<{ title: string; hint: string }>();
</script>

<template>
  <details class="group/fold rounded-xl border border-line">
    <summary class="flex cursor-pointer list-none items-center gap-3 rounded-xl px-4 py-3.5 hover:bg-raised focus-visible:outline-2 focus-visible:outline-accent">
      <span class="min-w-0 flex-1">
        <span class="block text-sm font-medium text-ink">{{ title }}</span>
        <span class="mt-0.5 block text-xs text-ink-muted">{{ hint }}</span>
      </span>
      <AppIcon name="chevron-right" size="sm" class="shrink-0 text-ink-muted transition-transform group-open/fold:rotate-90 motion-reduce:transition-none" />
    </summary>
    <div class="border-t border-line px-4"><slot /></div>
  </details>
</template>
