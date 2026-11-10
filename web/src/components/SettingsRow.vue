<script setup lang="ts">
import { useId } from 'vue';

/** One setting: its name and what it does on the left, its control on the right (below it when stacked or narrow). */
defineProps<{ label: string; description?: string; labelFor?: string; stacked?: boolean }>();
const descriptionId = useId();
</script>

<template>
  <div
    class="flex flex-col gap-x-6 gap-y-2 py-4"
    :class="{ 'min-[560px]:flex-row min-[560px]:items-start min-[560px]:justify-between': !stacked }"
  >
    <div class="min-w-0 flex-1">
      <label v-if="labelFor" :for="labelFor" class="block text-sm font-medium text-ink">{{ label }}</label>
      <span v-else class="block text-sm font-medium text-ink">{{ label }}</span>
      <p v-if="description" :id="descriptionId" class="mt-0.5 text-xs leading-relaxed text-ink-muted">{{ description }}</p>
    </div>
    <div class="flex min-w-0 flex-col gap-1.5" :class="{ 'min-[560px]:items-end': !stacked }">
      <slot :description-id="description ? descriptionId : undefined" />
    </div>
  </div>
</template>
