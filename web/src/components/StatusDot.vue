<script setup lang="ts">
import type { AssistantStatus } from '../api/types';
import { STATUS_LABELS } from '../lib/assistantRequests';

defineProps<{ status: AssistantStatus }>();
</script>

<template>
  <span
    class="inline-block size-2 shrink-0 rounded-full"
    :class="{
      'bg-lavender motion-safe:animate-pulse': status === 'queued' || status === 'analyzing' || status === 'applying',
      'bg-warn': status === 'awaiting-user',
      'bg-review': status === 'ready',
      'bg-ok': status === 'done',
      'bg-ink-faint': status === 'ignored',
      'bg-fail': status === 'failed',
    }"
    role="img"
    :aria-label="STATUS_LABELS[status]"
    :title="STATUS_LABELS[status]"
  />
</template>
