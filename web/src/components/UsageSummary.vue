<script setup lang="ts">
import { computed } from 'vue';
import type { Tokens, Usage, UsageWindow } from '../api/types';
import { compactCount, percent, untilTime } from '../lib/format';
import { totalTokens, usageLevel, usageRows, type WindowKey } from '../lib/usage';
import { t } from '../i18n';

const props = defineProps<{ usage: Usage }>();

const rows = computed(() => usageRows(props.usage));

function windowTitle(name: string, key: WindowKey, window: UsageWindow): string {
  const values = { name, used: percent(window.usedPercent), window: t(`home.${key}`) };
  return window.resetsAt
    ? t('home.windowTitle', { ...values, when: untilTime(window.resetsAt) })
    : t('home.windowTitleNoReset', values);
}

const tokensTitle = (tokens: Tokens): string =>
  t('home.tokensTitle', { input: compactCount(tokens.input), cached: compactCount(tokens.cached), output: compactCount(tokens.output) });
</script>

<template>
  <section class="mx-3 mb-3 flex flex-col gap-3 rounded-xl border border-line bg-surface p-3" :aria-label="$t('home.usageHeading')" data-testid="usage-summary">
    <div class="flex items-baseline justify-between gap-2">
      <p class="text-2xs font-medium uppercase tracking-wider text-ink-dim">{{ $t('home.usageHeading') }}</p>
      <p class="text-xs tabular-nums text-ink-muted" :title="usage.limit === null ? $t('home.usageTitle') : $t('home.usageTitleLimit')">
        {{ usage.limit === null ? $t('home.usage', { today: usage.today }) : $t('home.usageLimit', { today: usage.today, limit: usage.limit }) }}
      </p>
    </div>

    <ul v-if="rows.length > 0" class="flex flex-col gap-3" :aria-label="$t('home.limitsLabel')" data-testid="usage-limits">
      <li v-for="row in rows" :key="row.provider" class="flex flex-col gap-1.5">
        <div class="flex items-baseline justify-between gap-2">
          <p class="text-sm font-medium text-ink">{{ row.name }}</p>
          <p v-if="row.tokens" class="text-xs tabular-nums text-ink-muted" :title="tokensTitle(row.tokens)">
            {{ $t('home.tokensToday', { tokens: compactCount(totalTokens(row.tokens)) }) }}
          </p>
        </div>
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
              :class="{
                'bg-lavender': usageLevel(item.window) === 'normal',
                'bg-warn': usageLevel(item.window) === 'high',
                'bg-fail': usageLevel(item.window) === 'critical',
              }"
              :style="{ width: `${item.window.usedPercent}%` }"
            />
          </span>
          <span
            class="text-right tabular-nums"
            :class="{
              'text-ink-muted': usageLevel(item.window) === 'normal',
              'font-medium text-warn': usageLevel(item.window) === 'high',
              'font-semibold text-fail': usageLevel(item.window) === 'critical',
            }"
            aria-hidden="true"
          >
            {{ percent(item.window.usedPercent) }}
          </span>
        </div>
      </li>
    </ul>
  </section>
</template>
