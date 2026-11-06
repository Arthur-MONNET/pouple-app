<script setup lang="ts">
import type { GuideStep } from '../api/types';
import AppButton from './AppButton.vue';
import AppIcon from './AppIcon.vue';
import { linkTarget } from '../lib/guideLink';

const props = defineProps<{ steps: GuideStep[]; /** What the user typed so far, to point a link at their own server. */ values?: Record<string, string> }>();

/** The native shell routes a new window to the default browser. */
const open = (link: NonNullable<GuideStep['link']>): void => void window.open(linkTarget(link, props.values), '_blank', 'noopener');
</script>

<template>
  <ol class="flex flex-col gap-5" :aria-label="$t('guide.label')">
    <li v-for="(step, index) in steps" :key="step.title" class="flex gap-3.5">
      <span class="flex size-7 shrink-0 items-center justify-center rounded-full border border-line-bright bg-raised font-mono text-xs text-ink-muted" aria-hidden="true">
        {{ index + 1 }}
      </span>
      <div class="flex min-w-0 flex-col gap-1.5 pt-0.5">
        <h3 class="text-sm font-medium text-ink"><span class="sr-only">{{ $t('guide.step', { n: index + 1 }) }}</span>{{ step.title }}</h3>
        <p class="text-sm leading-relaxed text-ink-muted">{{ step.body }}</p>
        <AppButton v-if="step.link" variant="outline" size="sm" class="mt-1 w-fit" @click="open(step.link)">
          {{ step.link.label }}
          <AppIcon name="external" size="sm" />
        </AppButton>
      </div>
    </li>
  </ol>
</template>
