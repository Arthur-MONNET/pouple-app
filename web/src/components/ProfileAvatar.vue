<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { initials } from '../lib/format';

const props = withDefaults(defineProps<{ name: string; url?: string; size?: 'md' | 'lg' }>(), { size: 'md' });
const broken = ref(false);
watch(() => props.url, () => (broken.value = false));
const showImage = computed(() => Boolean(props.url) && !broken.value);
</script>

<template>
  <span
    class="flex shrink-0 items-center justify-center overflow-hidden rounded-xl bg-selected font-semibold text-ink-muted"
    :class="size === 'lg' ? 'size-16 text-title' : 'size-10 text-sm'"
  >
    <img v-if="showImage" :src="url" alt="" referrerpolicy="no-referrer" class="size-full object-cover" @error="broken = true" />
    <span v-else aria-hidden="true">{{ initials(name) }}</span>
  </span>
</template>
