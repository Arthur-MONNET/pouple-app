<script setup lang="ts">
import { computed, ref } from 'vue';

const props = withDefaults(defineProps<{ length: number; limit?: number }>(), { limit: 420 });

const open = ref(false);
/** Short answers are shown whole; a long one is folded, so a wall of text never fills the conversation. */
const folded = computed(() => props.length > props.limit && !open.value);
const foldable = computed(() => props.length > props.limit);
</script>

<template>
  <div>
    <div class="relative" :class="{ 'max-h-40 overflow-hidden': folded }">
      <slot />
      <div v-if="folded" class="pointer-events-none absolute inset-x-0 bottom-0 h-10 bg-gradient-to-t from-raised to-transparent" aria-hidden="true" />
    </div>
    <button
      v-if="foldable"
      type="button"
      class="mt-1 text-xs text-ink-dim transition hover:text-ink motion-reduce:transition-none"
      :aria-expanded="open"
      @click="open = !open"
    >
      {{ open ? 'Show less' : 'Show more' }}
    </button>
  </div>
</template>
