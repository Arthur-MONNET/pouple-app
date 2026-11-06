<script setup lang="ts">
import AppIcon from './AppIcon.vue';

/** An error in the fail colour. It always carries an icon, so it never relies on colour alone. */
withDefaults(defineProps<{ box?: boolean }>(), { box: false });
</script>

<template>
  <p role="alert" class="flex items-start gap-1.5 text-fail" :class="box ? 'rounded-lg border border-fail/40 bg-fail/10 px-3 py-2 text-sm' : 'text-xs'">
    <AppIcon name="alert" size="sm" class="mt-0.5" />
    <span class="min-w-0"><slot /></span>
  </p>
</template>
