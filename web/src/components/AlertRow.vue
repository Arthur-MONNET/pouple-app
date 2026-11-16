<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue';
import type { AlertPatch, AssistantAlert } from '../api/types';
import { formatClock, isUpcoming, toLocalInput } from '../lib/alerts';
import { untilTime } from '../lib/format';
import AppButton from './AppButton.vue';
import IconButton from './IconButton.vue';

const props = withDefaults(defineProps<{ alert: AssistantAlert; busy?: boolean; hasConversation?: boolean }>(), { busy: false, hasConversation: false });
const emit = defineEmits<{ update: [patch: AlertPatch]; remove: []; open: [] }>();

const ids = { title: useId(), note: useId(), at: useId() };

const editing = ref(false);
const title = ref(props.alert.title);
const note = ref(props.alert.note ?? '');
const at = ref(toLocalInput(props.alert.at));

/** What the core holds: the drafts start from it, and go back to it when the user cancels. */
function reset(): void {
  title.value = props.alert.title;
  note.value = props.alert.note ?? '';
  at.value = toLocalInput(props.alert.at);
}

// The core is the truth: a change that came back (or was made elsewhere) replaces what was typed and closes the editor.
watch(
  () => [props.alert.title, props.alert.note, props.alert.at],
  () => {
    reset();
    editing.value = false;
  },
);

/** Only what differs from the alert is sent, so saving a new title never moves the alert. */
const patch = computed<AlertPatch>(() => ({
  ...(title.value.trim() !== props.alert.title && { title: title.value.trim() }),
  ...(note.value.trim() !== (props.alert.note ?? '') && { note: note.value.trim() }),
  ...(at.value !== toLocalInput(props.alert.at) && { at: at.value }),
}));
const changed = computed(() => Object.keys(patch.value).length > 0);
const titleOk = computed(() => title.value.trim() !== '');
/** The moment must be in the future only when it is the one being changed. */
const atOk = computed(() => at.value === toLocalInput(props.alert.at) || isUpcoming(at.value));
const canSave = computed(() => changed.value && titleOk.value && atOk.value && !props.busy);

function save(): void {
  if (canSave.value) emit('update', patch.value);
}

function cancel(): void {
  reset();
  editing.value = false;
}
</script>

<template>
  <li class="flex flex-col gap-2 py-3" data-testid="alert-row">
    <div class="flex items-start gap-3">
      <button
        type="button"
        class="w-14 shrink-0 rounded-md bg-raised px-1.5 py-1 text-center font-mono text-sm text-ink transition hover:bg-selected motion-reduce:transition-none"
        :aria-label="$t('alerts.row.edit', { title: alert.title })"
        :aria-expanded="editing"
        @click="editing = !editing"
      >
        {{ formatClock(alert.at) }}
      </button>
      <div class="min-w-0 flex-1">
        <p class="break-words font-medium text-ink">{{ alert.title }}</p>
        <p v-if="alert.note" class="whitespace-pre-line break-words text-xs text-ink-muted">{{ alert.note }}</p>
        <p class="mt-0.5 text-2xs text-ink-dim">
          {{ untilTime(alert.at) }}
          <template v-if="hasConversation">
            ·
            <button type="button" class="underline decoration-line-bright underline-offset-2 hover:text-ink" @click="emit('open')">{{ $t('alerts.row.fromConversation') }}</button>
          </template>
        </p>
      </div>
      <IconButton :label="$t('alerts.row.edit', { title: alert.title })" icon="pencil" size="sm" :disabled="busy" data-testid="alert-edit" @click="editing = !editing" />
      <IconButton :label="$t('alerts.row.delete', { title: alert.title })" icon="trash" size="sm" :disabled="busy" data-testid="alert-delete" @click="emit('remove')" />
    </div>
    <form v-if="editing" class="flex flex-col gap-2.5 rounded-lg border border-line bg-surface p-3" data-testid="alert-edit-form" @submit.prevent="save" @keydown.esc="cancel">
      <div class="flex flex-col gap-1">
        <label :for="ids.title" class="text-xs font-medium text-ink-muted">{{ $t('alerts.add.titleLabel') }}</label>
        <input
          :id="ids.title"
          v-model="title"
          type="text"
          maxlength="120"
          autocomplete="off"
          :aria-invalid="!titleOk"
          class="h-8 rounded-md border bg-canvas px-2.5 text-sm text-ink outline-none focus:border-accent"
          :class="{ 'border-fail/70': !titleOk, 'border-line-strong': titleOk }"
        />
      </div>
      <div class="flex flex-col gap-1">
        <label :for="ids.note" class="text-xs font-medium text-ink-muted">{{ $t('alerts.add.noteLabel') }}</label>
        <!-- A textarea, not a one-line field: a suggested reminder's note may span lines, and a text field drops its breaks. -->
        <textarea
          :id="ids.note"
          v-model="note"
          rows="2"
          maxlength="1000"
          autocomplete="off"
          :placeholder="$t('alerts.add.notePlaceholder')"
          class="resize-y rounded-md border border-line-strong bg-canvas px-2.5 py-1.5 text-sm text-ink outline-none focus:border-accent"
        />
      </div>
      <div class="flex flex-col gap-1">
        <label :for="ids.at" class="text-xs font-medium text-ink-muted">{{ $t('alerts.add.atLabel') }}</label>
        <input
          :id="ids.at"
          v-model="at"
          type="datetime-local"
          :aria-invalid="!atOk"
          class="h-8 w-fit rounded-md border bg-canvas px-2 text-xs text-ink outline-none focus:border-accent"
          :class="{ 'border-fail/70': !atOk, 'border-line-strong': atOk }"
        />
        <p v-if="!atOk" class="text-xs text-fail" role="alert">{{ $t('alerts.add.pastError') }}</p>
      </div>
      <div class="flex items-center gap-2">
        <AppButton type="submit" variant="primary" size="sm" :disabled="!canSave" data-testid="alert-save">{{ $t('alerts.row.save') }}</AppButton>
        <AppButton variant="ghost" size="sm" @click="cancel">{{ $t('alerts.row.undo') }}</AppButton>
      </div>
    </form>
  </li>
</template>
