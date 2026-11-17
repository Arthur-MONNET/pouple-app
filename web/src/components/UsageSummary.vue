<script setup lang="ts">
import { computed } from 'vue';
import type { MachineLevel, Tokens, Usage, UsageWindow } from '../api/types';
import { compactCount, gigabytes, percent, untilTime } from '../lib/format';
import { machineGauges, totalTokens, usageLevel, usageRows, type MachineGauge, type WindowKey } from '../lib/usage';
import { t } from '../i18n';

const props = defineProps<{ usage: Usage }>();

const rows = computed(() => usageRows(props.usage));
const gauges = computed(() => machineGauges(props.usage.machine));
const disk = computed(() => props.usage.machine?.disk);

function gaugeTitle(gauge: MachineGauge): string {
  const { disk: diskState, memory } = props.usage.machine ?? {};
  if (gauge.key === 'disk' && diskState) {
    return t('home.diskTitle', { free: gigabytes(diskState.freeBytes), total: gigabytes(diskState.totalBytes) });
  }
  return t('home.memoryTitle', { used: percent(gauge.usedPercent), total: memory ? gigabytes(memory.totalBytes) : '?' });
}

const barClass = (level: MachineLevel): Record<string, boolean> => ({
  'bg-lavender': level === 'normal',
  'bg-warn': level === 'high',
  'bg-fail': level === 'critical',
});

const valueClass = (level: MachineLevel): Record<string, boolean> => ({
  'text-ink-muted': level === 'normal',
  'font-medium text-warn': level === 'high',
  'font-semibold text-fail': level === 'critical',
});

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
              :class="barClass(usageLevel(item.window))"
              :style="{ width: `${item.window.usedPercent}%` }"
            />
          </span>
          <span
            class="text-right tabular-nums"
            :class="valueClass(usageLevel(item.window))"
            aria-hidden="true"
          >
            {{ percent(item.window.usedPercent) }}
          </span>
        </div>
      </li>
    </ul>

    <div v-if="gauges.length > 0" class="flex flex-col gap-1.5" role="group" :aria-label="$t('home.machineLabel')" data-testid="usage-machine">
      <div class="flex items-baseline justify-between gap-2">
        <p class="text-sm font-medium text-ink">{{ $t('home.machineName') }}</p>
        <p v-if="disk" class="text-xs tabular-nums" :class="valueClass(disk.level)">
          {{ $t('home.diskFree', { free: gigabytes(disk.freeBytes) }) }}
        </p>
      </div>
      <div
        v-for="gauge in gauges"
        :key="gauge.key"
        class="grid grid-cols-[3.25rem_minmax(0,1fr)_2.5rem] items-center gap-2 text-xs"
        :title="gaugeTitle(gauge)"
        :data-testid="`machine-${gauge.key}`"
      >
        <span class="sr-only">{{ gaugeTitle(gauge) }}</span>
        <span class="text-ink-dim" aria-hidden="true">{{ $t(`home.${gauge.key}Label`) }}</span>
        <span class="h-1.5 overflow-hidden rounded-full bg-raised" aria-hidden="true">
          <span
            class="block h-full rounded-full transition-[width] motion-reduce:transition-none"
            :class="barClass(gauge.level)"
            :style="{ width: `${gauge.usedPercent}%` }"
          />
        </span>
        <span class="text-right tabular-nums" :class="valueClass(gauge.level)" aria-hidden="true">{{ percent(gauge.usedPercent) }}</span>
      </div>
    </div>
  </section>
</template>
