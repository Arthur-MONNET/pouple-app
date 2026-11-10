<script setup lang="ts">
import { useId } from 'vue';

defineProps<{ title: string; description?: string }>();
const titleId = useId();
</script>

<template>
  <section :aria-labelledby="titleId" class="flex flex-col">
    <h2 :id="titleId" class="text-base font-semibold text-ink">{{ title }}</h2>
    <p v-if="description" class="mt-1 text-xs leading-relaxed text-ink-muted">{{ description }}</p>
    <!-- The rows of a section are told apart by a line, as the settings of a desktop app are. -->
    <div class="mt-3 flex flex-col divide-y divide-line border-t border-line">
      <slot />
    </div>
    <!-- Outside the divided rows: a status line is not a setting, so it gets no line of its own. -->
    <slot name="footer" />
  </section>
</template>
