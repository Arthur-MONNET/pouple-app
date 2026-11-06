<script setup lang="ts">
import icon from '../assets/mascot/app-icon.svg';

withDefaults(defineProps<{ size?: 'md' | 'lg' }>(), { size: 'md' });
</script>

<template>
  <span class="inline-flex items-center font-display font-semibold tracking-tight text-ink" :class="size === 'lg' ? 'gap-3 text-3xl' : 'gap-2 text-xl'">
    <img :src="icon" alt="" draggable="false" :class="size === 'lg' ? 'size-10' : 'size-7'" />
    Pouple
  </span>
</template>
