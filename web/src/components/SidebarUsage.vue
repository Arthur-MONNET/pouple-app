<script setup lang="ts">
import { computed, ref, useId } from 'vue';
import type { Usage } from '../api/types';
import { gigabytes, percent } from '../lib/format';
import { headlineLevel, usageHeadline } from '../lib/usage';
import AppIcon from './AppIcon.vue';
import UsageSummary from './UsageSummary.vue';

const props = defineProps<{ usage: Usage }>();

const open = ref(false);
const panelId = useId();
const items = computed(() => usageHeadline(props.usage));
const level = computed(() => headlineLevel(items.value));
</script>

<template>
  <div class="shrink-0 border-t border-line-soft p-2" data-testid="sidebar-usage">
    <div v-if="open" :id="panelId" class="mb-1">
      <UsageSummary :usage="usage" />
    </div>
    <button
      type="button"
      class="flex w-full flex-col gap-1 rounded-md px-2 py-1.5 text-left text-xs transition hover:bg-raised motion-reduce:transition-none"
      :aria-expanded="open"
      :aria-controls="panelId"
      :title="open ? $t('home.usageHide') : $t('home.usageShow')"
      @click="open = !open"
    >
      <span class="flex w-full items-center gap-2">
        <span
          class="size-1.5 shrink-0 rounded-full"
          :class="{ 'bg-ok': level === 'normal', 'bg-warn': level === 'high', 'bg-fail': level === 'critical' }"
          aria-hidden="true"
        />
        <span class="flex-1 text-ink-dim">{{ $t('home.usageHeading') }}</span>
        <span class="tabular-nums text-ink-muted">
          {{ usage.limit === null ? $t('home.usage', { today: usage.today }) : $t('home.usageLimit', { today: usage.today, limit: usage.limit }) }}
        </span>
        <AppIcon :name="open ? 'chevron-down' : 'chevron-up'" size="sm" class="text-ink-dim" />
      </span>
      <span v-if="items.length > 0" class="flex flex-wrap items-center gap-x-3 gap-y-0.5 pl-3.5">
        <span
          v-for="item in items"
          :key="item.key"
          class="inline-flex items-center gap-1 tabular-nums"
          :class="{ 'text-ink-muted': item.level === 'normal', 'text-warn': item.level === 'high', 'font-medium text-fail': item.level === 'critical' }"
        >
          <span class="text-ink-dim">{{ item.key === 'disk' ? $t('home.diskLabel') : item.key === 'memory' ? $t('home.memoryLabel') : item.name }}</span>
          {{ item.freeBytes !== undefined ? gigabytes(item.freeBytes) : percent(item.usedPercent ?? 0) }}
        </span>
      </span>
    </button>
  </div>
</template>
