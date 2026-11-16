<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import type { ReminderOffer, ReminderPick } from '../api/types';
import { isUpcoming, toLocalInput } from '../lib/alerts';
import AppButton from './AppButton.vue';
import AppIcon from './AppIcon.vue';

const props = withDefaults(defineProps<{ title: string; offer: ReminderOffer; busy?: boolean }>(), { busy: false });
const emit = defineEmits<{ confirm: [items: ReminderPick[]]; skip: [] }>();

/** What the user did so far: the options they ticked, and the moment they want for each. */
const ticked = ref<string[]>([]);
const times = ref<Record<string, string>>({});

// A new suggestion starts over: nothing ticked, each option at the moment the assistant proposed.
watch(
  () => props.offer.id,
  () => {
    ticked.value = [];
    times.value = Object.fromEntries(props.offer.items.map((item) => [item.id, toLocalInput(item.at)]));
  },
  { immediate: true },
);

const timeOf = (id: string): string => times.value[id] ?? '';
const isTicked = (id: string): boolean => ticked.value.includes(id);

/** The ticked options, in the order they are listed. */
const chosen = computed(() => props.offer.items.filter((item) => isTicked(item.id)));
const allUpcoming = computed(() => chosen.value.every((item) => isUpcoming(timeOf(item.id))));
const canConfirm = computed(() => chosen.value.length > 0 && allUpcoming.value && !props.busy);

function toggle(id: string): void {
  if (props.busy) return;
  ticked.value = isTicked(id) ? ticked.value.filter((item) => item !== id) : [...ticked.value, id];
}

function setTime(id: string, value: string): void {
  times.value = { ...times.value, [id]: value };
  // Changing the time of an option is choosing it.
  if (!isTicked(id)) ticked.value = [...ticked.value, id];
}

const confirm = (): void => {
  if (canConfirm.value) emit('confirm', chosen.value.map((item) => ({ id: item.id, at: timeOf(item.id) })));
};
</script>

<template>
  <section class="flex flex-col gap-2" data-testid="reminder-offer">
    <span class="w-fit max-w-full truncate rounded-lg bg-raised px-3 py-1.5 text-xs text-ink">{{ title }}</span>
    <div class="rounded-xl border border-line bg-surface p-3">
      <p class="mb-1 flex items-center gap-1.5 font-medium text-ink">
        <AppIcon name="bell" />
        {{ $t('chat.reminders.question') }}
      </p>
      <p class="mb-3 text-xs text-ink-muted">{{ $t('chat.reminders.hint') }}</p>
      <ul class="flex flex-col gap-1.5" role="group" :aria-label="$t('chat.reminders.title')">
        <li
          v-for="item in offer.items"
          :key="item.id"
          class="flex flex-wrap items-center gap-2 rounded-lg border px-2.5 py-2 transition motion-reduce:transition-none"
          :class="{ 'border-accent bg-selected': isTicked(item.id), 'border-line': !isTicked(item.id) }"
        >
          <button
            type="button"
            role="checkbox"
            :aria-checked="isTicked(item.id)"
            :aria-label="$t('chat.reminders.pickLabel', { label: item.label })"
            class="flex min-w-0 flex-1 items-start gap-2.5 rounded-md text-left"
            @click="toggle(item.id)"
          >
            <span
              class="mt-px flex size-5 shrink-0 items-center justify-center rounded border border-line-strong text-ink"
              :class="{ 'border-accent bg-accent text-canvas': isTicked(item.id) }"
            >
              <AppIcon v-if="isTicked(item.id)" name="check" size="sm" />
            </span>
            <span class="min-w-0">
              <span class="block break-words text-ink">{{ item.label }}</span>
              <span v-if="item.note" class="block break-words text-xs text-ink-muted">{{ item.note }}</span>
            </span>
          </button>
          <input
            :value="timeOf(item.id)"
            type="datetime-local"
            :aria-label="$t('chat.reminders.timeLabel', { label: item.label })"
            :aria-invalid="isTicked(item.id) && !isUpcoming(timeOf(item.id))"
            class="h-8 rounded-md border bg-canvas px-2 text-xs text-ink outline-none focus:border-accent"
            :class="{ 'border-fail/70': isTicked(item.id) && !isUpcoming(timeOf(item.id)), 'border-line-strong': !isTicked(item.id) || isUpcoming(timeOf(item.id)) }"
            @input="setTime(item.id, ($event.target as HTMLInputElement).value)"
          />
        </li>
      </ul>
      <p v-if="chosen.length > 0 && !allUpcoming" class="mt-2 text-xs text-fail" role="alert">{{ $t('chat.reminders.pastError') }}</p>
      <div class="mt-3 flex items-center gap-2">
        <AppButton variant="ghost" size="sm" :disabled="busy" data-testid="reminders-skip" @click="emit('skip')">{{ $t('chat.reminders.skip') }}</AppButton>
        <AppButton variant="primary" size="sm" class="ml-auto" :disabled="!canConfirm" data-testid="reminders-confirm" @click="confirm">
          {{ chosen.length === 0 ? $t('chat.reminders.confirmNone') : $t('chat.reminders.confirm', { n: chosen.length }, chosen.length) }}
        </AppButton>
      </div>
    </div>
  </section>
</template>
