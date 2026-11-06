<script setup lang="ts">
import AppIcon from './AppIcon.vue';

export interface StepperItem {
  id: string;
  name: string;
  connected: boolean;
}

defineProps<{ items: StepperItem[]; current: number }>();
</script>

<template>
  <nav aria-label="Setup progress">
    <ol class="flex items-center gap-2">
      <li v-for="(item, index) in items" :key="item.id" class="flex items-center gap-2" :aria-current="index === current ? 'step' : undefined">
        <span
          class="flex size-6 shrink-0 items-center justify-center rounded-full border text-xs font-medium transition motion-reduce:transition-none"
          :class="{
            'border-ok bg-ok/15 text-ok': item.connected,
            'border-accent bg-accent text-canvas': !item.connected && index === current,
            'border-line-bright text-ink-dim': !item.connected && index !== current,
          }"
        >
          <AppIcon v-if="item.connected" name="check" size="sm" />
          <span v-else aria-hidden="true">{{ index + 1 }}</span>
        </span>
        <span class="text-sm" :class="index === current ? 'font-medium text-ink' : 'text-ink-dim'">
          {{ item.name }}<span class="sr-only">{{ item.connected ? ' (connected)' : index === current ? ' (current step)' : ' (to do)' }}</span>
        </span>
        <span v-if="index < items.length - 1" class="mx-1 h-px w-8 bg-line-strong min-[700px]:w-14" aria-hidden="true" />
      </li>
      <li v-for="(label, offset) in ['Projects', 'Ready']" :key="label" class="flex items-center gap-2 text-ink-dim" :aria-current="current === items.length + offset ? 'step' : undefined">
        <span class="mx-1 h-px w-8 bg-line-strong min-[700px]:w-14" aria-hidden="true" />
        <span class="text-sm" :class="current === items.length + offset ? 'font-medium text-ink' : ''">{{ label }}</span>
      </li>
    </ol>
  </nav>
</template>
