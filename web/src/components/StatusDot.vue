<script setup lang="ts">
import type { AssistantStatus } from '../api/types';
import { statusLabel } from '../lib/assistantRequests';

defineProps<{ status: AssistantStatus }>();
</script>

<template>
  <span
    class="inline-block size-2 shrink-0 rounded-full"
    :class="{
      'bg-lavender motion-safe:animate-pulse': status === 'queued' || status === 'analyzing' || status === 'applying',
      'bg-accent': status === 'awaiting-user',
      'bg-ink-muted': status === 'ready',
      'bg-ok': status === 'done',
      'bg-ink-faint': status === 'ignored',
      'bg-fail': status === 'failed',
    }"
    role="img"
    :aria-label="statusLabel(status)"
    :title="statusLabel(status)"
  />
</template>
