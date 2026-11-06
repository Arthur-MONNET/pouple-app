<script setup lang="ts">
import AppIcon, { type IconName } from './AppIcon.vue';

withDefaults(
  defineProps<{
    label: string;
    icon: IconName;
    size?: 'sm' | 'md';
    bordered?: boolean;
    disabled?: boolean;
  }>(),
  { size: 'md', bordered: false, disabled: false },
);
</script>

<template>
  <button
    type="button"
    :aria-label="label"
    :title="label"
    :disabled="disabled"
    class="inline-flex shrink-0 items-center justify-center rounded-md text-ink-muted transition hover:text-ink disabled:cursor-not-allowed disabled:opacity-50 motion-reduce:transition-none"
    :class="{
      'size-7': size === 'sm',
      'size-8': size === 'md',
      'border border-line bg-surface hover:bg-raised': bordered,
      'hover:bg-raised': !bordered,
    }"
  >
    <AppIcon :name="icon" />
  </button>
</template>
