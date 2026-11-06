<script setup lang="ts">
import PouMascot, { type MascotPose } from './PouMascot.vue';

/** The dim stage the mascot stands on outside the welcome and final screens (those two use lavender). */
withDefaults(defineProps<{ pose: MascotPose; size?: 'sm' | 'md' | 'lg' }>(), { size: 'md' });
</script>

<template>
  <span
    class="flex shrink-0 items-center justify-center rounded-xl border border-line-strong bg-stage"
    :class="{ 'size-12 p-1': size === 'sm', 'size-20 p-1.5': size === 'md', 'h-56 w-full p-3': size === 'lg' }"
    aria-hidden="true"
  >
    <PouMascot :pose="pose" class="h-full max-w-full" />
  </span>
</template>
