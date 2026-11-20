<script setup lang="ts">
import type { RouteLocationRaw } from 'vue-router';
import AppIcon, { type IconName } from './AppIcon.vue';

withDefaults(
  defineProps<{
    to: RouteLocationRaw;
    icon: IconName;
    label: string;
    active: boolean;
    /** A number shown after the label; nothing for zero. */
    count?: number;
    /** The count asks for the user (decisions waiting), so it takes the accent. */
    urgent?: boolean;
    /** A dot that says something new is there, with what it says for a screen reader. */
    dotLabel?: string;
  }>(),
  { count: 0, urgent: false, dotLabel: undefined },
);
</script>

<template>
  <RouterLink v-slot="{ href, navigate }" :to="to" custom>
    <a
      :href="href"
      class="flex h-8 items-center gap-2.5 rounded-md px-2.5 text-sm transition motion-reduce:transition-none"
      :class="{ 'bg-selected text-ink': active, 'text-ink-muted hover:bg-raised hover:text-ink': !active }"
      :aria-current="active ? 'page' : undefined"
      @click="navigate"
    >
      <AppIcon :name="icon" />
      <span class="min-w-0 flex-1 truncate">{{ label }}</span>
      <span v-if="dotLabel" class="size-1.5 shrink-0 rounded-full bg-accent" role="img" :aria-label="dotLabel" :title="dotLabel" />
      <span
        v-if="count > 0"
        class="shrink-0 rounded px-1.5 text-2xs tabular-nums"
        :class="{ 'bg-accent/20 font-medium text-accent': urgent, 'text-ink-dim': !urgent }"
      >
        {{ count }}
      </span>
    </a>
  </RouterLink>
</template>
