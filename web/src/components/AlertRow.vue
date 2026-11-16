<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import type { AssistantAlert } from '../api/types';
import { formatClock, isUpcoming, toLocalInput } from '../lib/alerts';
import { untilTime } from '../lib/format';
import AppButton from './AppButton.vue';
import IconButton from './IconButton.vue';

const props = withDefaults(defineProps<{ alert: AssistantAlert; busy?: boolean; hasConversation?: boolean }>(), { busy: false, hasConversation: false });
const emit = defineEmits<{ move: [at: string]; remove: []; open: [] }>();

const editing = ref(false);
const draft = ref(toLocalInput(props.alert.at));
const valid = computed(() => isUpcoming(draft.value));

// The core is the truth: a moment changed elsewhere replaces what was being typed.
watch(
  () => props.alert.at,
  (at) => {
    draft.value = toLocalInput(at);
    editing.value = false;
  },
);

function save(): void {
  if (valid.value) emit('move', draft.value);
}

function cancel(): void {
  draft.value = toLocalInput(props.alert.at);
  editing.value = false;
}
</script>

<template>
  <li class="flex flex-col gap-2 py-3" data-testid="alert-row">
    <div class="flex items-start gap-3">
      <button
        type="button"
        class="w-14 shrink-0 rounded-md bg-raised px-1.5 py-1 text-center font-mono text-sm text-ink transition hover:bg-selected motion-reduce:transition-none"
        :aria-label="$t('alerts.row.timeLabel', { title: alert.title })"
        :aria-expanded="editing"
        @click="editing = !editing"
      >
        {{ formatClock(alert.at) }}
      </button>
      <div class="min-w-0 flex-1">
        <p class="break-words font-medium text-ink">{{ alert.title }}</p>
        <p v-if="alert.note" class="break-words text-xs text-ink-muted">{{ alert.note }}</p>
        <p class="mt-0.5 text-2xs text-ink-dim">
          {{ untilTime(alert.at) }}
          <template v-if="hasConversation">
            ·
            <button type="button" class="underline decoration-line-bright underline-offset-2 hover:text-ink" @click="emit('open')">{{ $t('alerts.row.fromConversation') }}</button>
          </template>
        </p>
      </div>
      <IconButton :label="$t('alerts.row.delete', { title: alert.title })" icon="trash" size="sm" :disabled="busy" data-testid="alert-delete" @click="emit('remove')" />
    </div>
    <div v-if="editing" class="flex flex-wrap items-center gap-2 pl-[4.25rem]">
      <input
        v-model="draft"
        type="datetime-local"
        :aria-label="$t('alerts.row.timeLabel', { title: alert.title })"
        :aria-invalid="!valid"
        class="h-8 rounded-md border bg-canvas px-2 text-xs text-ink outline-none focus:border-accent"
        :class="{ 'border-fail/70': !valid, 'border-line-strong': valid }"
        @keydown.enter.prevent="save"
        @keydown.esc="cancel"
      />
      <AppButton variant="primary" size="sm" :disabled="!valid || busy" data-testid="alert-save" @click="save">{{ $t('alerts.row.save') }}</AppButton>
      <AppButton variant="ghost" size="sm" @click="cancel">{{ $t('alerts.row.undo') }}</AppButton>
    </div>
  </li>
</template>
