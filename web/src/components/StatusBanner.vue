<script setup lang="ts">
import type { AssistantStatusInfo } from '../api/types';
import InlineError from './InlineError.vue';
import MascotStage from './MascotStage.vue';

/** What keeps Pouple from working, for every screen: the core out of reach, Slack failing, a pause, a notice. */
defineProps<{ status: AssistantStatusInfo | null; error: string | null }>();
</script>

<template>
  <div v-if="status?.problem || error || (status?.notices.length ?? 0) > 0" class="flex items-start gap-3 border-b border-line-soft bg-surface px-4 py-2 text-xs" aria-live="polite" data-testid="status-banner">
    <MascotStage v-if="status?.problem || error" pose="oops" size="sm" />
    <div class="flex min-w-0 flex-col justify-center gap-1 self-center">
      <InlineError v-if="error">{{ $t('home.coreDown', { error }) }}</InlineError>
      <InlineError v-if="status?.problem && status.active">{{ $t('home.slackFailed', { problem: status.problem }) }}</InlineError>
      <p v-else-if="status?.problem" role="status" class="text-ink-muted">{{ $t('home.paused', { problem: status.problem }) }}</p>
      <p v-for="notice in status?.notices ?? []" :key="notice" class="text-ink-muted">{{ notice }}</p>
    </div>
  </div>
</template>
