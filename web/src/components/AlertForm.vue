<script setup lang="ts">
import { computed, ref, useId } from 'vue';
import type { AlertInput } from '../api/types';
import { isUpcoming, nextHour } from '../lib/alerts';
import AppButton from './AppButton.vue';

const props = withDefaults(defineProps<{ busy?: boolean }>(), { busy: false });
const emit = defineEmits<{ submit: [input: AlertInput] }>();

const ids = { title: useId(), note: useId(), at: useId() };

const title = ref('');
const note = ref('');
const at = ref(nextHour());

const titleOk = computed(() => title.value.trim() !== '');
const atOk = computed(() => isUpcoming(at.value));
const canSubmit = computed(() => titleOk.value && atOk.value && !props.busy);

function submit(): void {
  if (!canSubmit.value) return;
  const trimmedNote = note.value.trim();
  emit('submit', { title: title.value.trim(), at: at.value, ...(trimmedNote && { note: trimmedNote }) });
}

/** The form starts over once the alert is made; the parent calls this when the core accepted it. */
function reset(): void {
  title.value = '';
  note.value = '';
  at.value = nextHour();
}

defineExpose({ reset });
</script>

<template>
  <form class="flex flex-col gap-3 rounded-xl border border-line bg-surface p-4" data-testid="alert-form" @submit.prevent="submit">
    <h2 class="text-base font-semibold text-ink">{{ $t('alerts.add.title') }}</h2>
    <div class="flex flex-col gap-1.5">
      <label :for="ids.title" class="text-sm font-medium text-ink">{{ $t('alerts.add.titleLabel') }}</label>
      <input
        :id="ids.title"
        v-model="title"
        type="text"
        maxlength="120"
        autocomplete="off"
        :placeholder="$t('alerts.add.titlePlaceholder')"
        class="h-9 rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent"
      />
    </div>
    <div class="flex flex-col gap-1.5">
      <label :for="ids.note" class="text-sm font-medium text-ink">
        {{ $t('alerts.add.noteLabel') }}<span class="ml-1.5 font-normal text-ink-muted">{{ $t('common.optional') }}</span>
      </label>
      <input
        :id="ids.note"
        v-model="note"
        type="text"
        maxlength="1000"
        autocomplete="off"
        :placeholder="$t('alerts.add.notePlaceholder')"
        class="h-9 rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent"
      />
    </div>
    <div class="flex flex-wrap items-end gap-3">
      <div class="flex flex-col gap-1.5">
        <label :for="ids.at" class="text-sm font-medium text-ink">{{ $t('alerts.add.atLabel') }}</label>
        <input
          :id="ids.at"
          v-model="at"
          type="datetime-local"
          :aria-invalid="!atOk"
          class="h-9 rounded-lg border bg-canvas px-3 text-sm text-ink outline-none focus:border-accent"
          :class="{ 'border-fail/70': !atOk, 'border-line-strong': atOk }"
        />
      </div>
      <AppButton type="submit" variant="primary" class="ml-auto" :disabled="!canSubmit" data-testid="alert-submit">{{ $t('alerts.add.submit') }}</AppButton>
    </div>
    <p v-if="!atOk" class="text-xs text-fail" role="alert">{{ $t('alerts.add.pastError') }}</p>
  </form>
</template>
