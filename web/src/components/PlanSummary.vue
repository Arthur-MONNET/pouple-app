<script setup lang="ts">
import type { AssistantPlan } from '../api/types';
import ClampedBox from './ClampedBox.vue';

defineProps<{ plan: AssistantPlan }>();
</script>

<template>
  <section class="flex flex-col gap-3 rounded-lg border border-line bg-surface p-3" :aria-label="$t('chat.plan.title')" data-testid="plan-summary">
    <header>
      <p class="text-2xs font-medium uppercase tracking-wider text-ink-dim">{{ $t('chat.plan.title') }}</p>
      <h3 class="mt-1 text-sm font-semibold text-ink">{{ plan.goal }}</h3>
    </header>
    <ul class="flex flex-col gap-2" :aria-label="$t('chat.plan.actions')">
      <li v-for="(action, index) in plan.actions" :key="index" class="flex flex-col gap-0.5 rounded-md bg-raised px-3 py-2 text-xs">
        <span v-if="action.scope.service === 'terminal'" class="font-medium text-ink">{{ $t(`chat.plan.kind.${action.kind}`) }} · {{ action.scope.name || $t('chat.plan.terminalScope') }}</span>
        <span v-else class="font-medium text-ink">{{ $t(`chat.plan.kind.${action.kind}`) }} · {{ action.scope.name ? `${action.scope.name} (${action.scope.id})` : action.scope.id }}</span>
        <span v-if="action.branch" class="font-mono text-ink">{{ action.branch }}<template v-if="action.base"> {{ action.kind === 'gitlab.merge-request' ? '→' : '←' }} {{ action.base }}</template></span>
        <span class="text-ink-muted">{{ action.summary }}</span>
        <template v-if="action.kind === 'asana.create-task'">
          <p class="whitespace-pre-wrap break-words font-semibold text-ink">{{ action.title }}</p>
          <pre v-if="action.description" class="min-w-0 whitespace-pre-wrap break-words rounded-sm bg-canvas p-2 text-ink">{{ action.description }}</pre>
          <p v-else class="text-ink-muted">{{ $t('chat.plan.emptyTaskDescription') }}</p>
        </template>
        <template v-else-if="action.kind === 'slite.write'">
          <p class="whitespace-pre-wrap break-words font-semibold text-ink">{{ action.title }}</p>
          <pre v-if="action.markdown" class="min-w-0 whitespace-pre-wrap break-words rounded-sm bg-canvas p-2 text-ink">{{ action.markdown }}</pre>
          <p v-else class="text-ink-muted">{{ $t('chat.plan.emptyPage') }}</p>
        </template>
      </li>
    </ul>
    <div>
      <p class="mb-1 text-xs font-medium text-ink-muted">{{ $t('chat.plan.brief') }}</p>
      <ClampedBox :length="plan.brief.length">
        <p class="whitespace-pre-wrap break-words text-xs text-ink-muted">{{ plan.brief }}</p>
      </ClampedBox>
    </div>
  </section>
</template>
