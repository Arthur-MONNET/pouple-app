<script setup lang="ts">
import { computed } from 'vue';
import type { Usage, UsageWindow } from '../api/types';
import { compactCount, percent, untilTime } from '../lib/format';
import { limitRows, nearlyUsed, totalTokens, type WindowKey } from '../lib/usage';
import { t } from '../i18n';

const props = defineProps<{ usage: Usage }>();

const rows = computed(() => limitRows(props.usage));
const tokens = computed(() => props.usage.tokensToday);
const showsTokens = computed(() => tokens.value !== undefined && totalTokens(tokens.value) > 0);

function windowTitle(name: string, key: WindowKey, window: UsageWindow): string {
  const values = { name, used: percent(window.usedPercent), window: t(`home.${key}`) };
  return window.resetsAt
    ? t('home.windowTitle', { ...values, when: untilTime(window.resetsAt) })
    : t('home.windowTitleNoReset', values);
}
</script>

<template>
  <section class="mx-3 mb-3 flex flex-col gap-3 rounded-xl border border-line bg-surface p-3" :aria-label="$t('home.usageHeading')" data-testid="usage-summary">
    <div class="flex flex-col gap-0.5">
      <div class="flex items-baseline justify-between gap-2">
        <p class="text-2xs font-medium uppercase tracking-wider text-ink-dim">{{ $t('home.usageHeading') }}</p>
        <p class="text-xs tabular-nums text-ink-muted" :title="usage.limit === null ? $t('home.usageTitle') : $t('home.usageTitleLimit')">
          {{ usage.limit === null ? $t('home.usage', { today: usage.today }) : $t('home.usageLimit', { today: usage.today, limit: usage.limit }) }}
        </p>
      </div>
      <p
        v-if="showsTokens && tokens"
        class="text-base font-semibold tabular-nums text-ink"
        :title="$t('home.tokensTitle', { input: compactCount(tokens.input), cached: compactCount(tokens.cached), output: compactCount(tokens.output) })"
      >
        {{ $t('home.tokensToday', { tokens: compactCount(totalTokens(tokens)) }) }}
      </p>
    </div>

    <ul v-if="rows.length > 0" class="flex flex-col gap-3" :aria-label="$t('home.limitsLabel')" data-testid="usage-limits">
      <li v-for="row in rows" :key="row.provider" class="flex flex-col gap-1.5">
        <p class="text-xs font-medium text-ink-muted" aria-hidden="true">{{ row.name }}</p>
        <div
          v-for="item in row.windows"
          :key="item.key"
          class="grid grid-cols-[3.25rem_minmax(0,1fr)_2.5rem] items-center gap-2 text-xs"
          :title="windowTitle(row.name, item.key, item.window)"
        >
          <span class="sr-only">{{ windowTitle(row.name, item.key, item.window) }}</span>
          <span class="text-ink-dim" aria-hidden="true">{{ $t(`home.${item.key}Label`) }}</span>
          <span class="h-1.5 overflow-hidden rounded-full bg-raised" aria-hidden="true">
            <span
              class="block h-full rounded-full transition-[width] motion-reduce:transition-none"
              :class="{ 'bg-warn': nearlyUsed(item.window), 'bg-lavender': !nearlyUsed(item.window) }"
              :style="{ width: `${item.window.usedPercent}%` }"
            />
          </span>
          <span
            class="text-right tabular-nums"
            :class="{ 'font-medium text-warn': nearlyUsed(item.window), 'text-ink-muted': !nearlyUsed(item.window) }"
            aria-hidden="true"
          >
            {{ percent(item.window.usedPercent) }}
          </span>
        </div>
      </li>
    </ul>
  </section>
</template>
