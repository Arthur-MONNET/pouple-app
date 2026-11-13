<script setup lang="ts">
import { computed } from 'vue';
import type { Usage, UsageWindow } from '../api/types';
import { compactCount, percent, untilTime } from '../lib/format';
import { limitRows, nearlyUsed, totalTokens, type WindowKey } from '../lib/usage';
import { t } from '../i18n';

const props = defineProps<{ usage: Usage }>();

const rows = computed(() => limitRows(props.usage));
const tokens = computed(() => props.usage.tokensToday);

function windowTitle(name: string, key: WindowKey, window: UsageWindow): string {
  const values = { name, used: percent(window.usedPercent), window: t(`home.${key}`) };
  return window.resetsAt
    ? t('home.windowTitle', { ...values, when: untilTime(window.resetsAt) })
    : t('home.windowTitleNoReset', values);
}
</script>

<template>
  <div class="flex flex-col gap-1 border-t border-line-soft px-4 py-2 font-mono text-2xs text-ink-dim">
    <p>
      <span :title="usage.limit === null ? $t('home.usageTitle') : $t('home.usageTitleLimit')">
        {{ usage.limit === null ? $t('home.usage', { today: usage.today }) : $t('home.usageLimit', { today: usage.today, limit: usage.limit }) }}
      </span>
      <template v-if="tokens && totalTokens(tokens) > 0">
        ·
        <span :title="$t('home.tokensTitle', { input: compactCount(tokens.input), cached: compactCount(tokens.cached), output: compactCount(tokens.output) })">
          {{ $t('home.tokensToday', { tokens: compactCount(totalTokens(tokens)) }) }}
        </span>
      </template>
    </p>
    <ul v-if="rows.length > 0" class="flex flex-col gap-1" :aria-label="$t('home.limitsLabel')" data-testid="usage-limits">
      <li v-for="row in rows" :key="row.provider" class="flex flex-wrap items-center gap-x-3 gap-y-1">
        <span class="text-ink-muted" aria-hidden="true">{{ row.name }}</span>
        <span v-for="item in row.windows" :key="item.key" class="flex items-center gap-1.5" :title="windowTitle(row.name, item.key, item.window)">
          <span class="sr-only">{{ windowTitle(row.name, item.key, item.window) }}</span>
          <span aria-hidden="true">{{ $t(`home.${item.key}`) }}</span>
          <span class="h-1 w-10 overflow-hidden rounded-full bg-raised" aria-hidden="true">
            <span
              class="block h-full rounded-full"
              :class="{ 'bg-warn': nearlyUsed(item.window), 'bg-ink-muted': !nearlyUsed(item.window) }"
              :style="{ width: `${item.window.usedPercent}%` }"
            />
          </span>
          <span aria-hidden="true" :class="{ 'text-warn': nearlyUsed(item.window) }">{{ percent(item.window.usedPercent) }}</span>
        </span>
      </li>
    </ul>
  </div>
</template>
