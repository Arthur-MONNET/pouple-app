<script setup lang="ts">
withDefaults(
  defineProps<{
    variant?: 'primary' | 'neutral' | 'outline' | 'ghost' | 'danger';
    size?: 'sm' | 'md' | 'lg';
    type?: 'button' | 'submit';
    disabled?: boolean;
  }>(),
  { variant: 'neutral', size: 'md', type: 'button', disabled: false },
);
</script>

<template>
  <button
    :type="type"
    :disabled="disabled"
    class="inline-flex shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-md transition disabled:cursor-not-allowed disabled:opacity-50 motion-reduce:transition-none"
    :class="{
      'bg-accent font-semibold text-canvas hover:bg-accent-hover': variant === 'primary',
      'border border-line-strong bg-raised text-ink hover:bg-selected': variant === 'neutral',
      'border border-line-strong text-ink hover:bg-raised': variant === 'outline',
      'text-ink-muted hover:bg-raised hover:text-ink': variant === 'ghost',
      'border border-fail/55 bg-transparent text-fail hover:bg-fail/10': variant === 'danger',
      'h-7 px-2.5 text-xs': size === 'sm',
      'h-8 px-3 text-sm': size === 'md',
      'h-9 px-3.5 text-sm': size === 'lg',
    }"
  >
    <slot />
  </button>
</template>
