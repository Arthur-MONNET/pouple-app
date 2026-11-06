<script setup lang="ts">
import { computed, ref, useId } from 'vue';
import type { Field } from '../api/types';
import AppIcon from './AppIcon.vue';

const props = defineProps<{ field: Field; error: string | null; disabled?: boolean }>();
const model = defineModel<string>({ required: true });
const emit = defineEmits<{ blur: []; enter: [] }>();

const id = useId();
const helpId = `${id}-help`;
const errorId = `${id}-error`;
const revealed = ref(false);
const pasteNote = ref<string | null>(null);
const describedBy = computed(() => [props.field.help ? helpId : null, props.error ? errorId : null].filter(Boolean).join(' ') || undefined);

async function paste(): Promise<void> {
  pasteNote.value = null;
  try {
    model.value = (await navigator.clipboard.readText()).trim();
  } catch {
    pasteNote.value = 'The clipboard is not available here: paste with Cmd+V instead.';
  }
}
</script>

<template>
  <div class="flex flex-col gap-1.5">
    <label :for="id" class="text-sm font-medium text-ink">{{ field.label }}</label>
    <div class="flex items-stretch gap-1.5">
      <div
        class="flex h-10 min-w-0 flex-1 items-center rounded-lg border bg-surface transition focus-within:border-accent motion-reduce:transition-none"
        :class="error ? 'border-fail/70' : 'border-line-strong'"
      >
        <input
          :id="id"
          v-model="model"
          :type="field.secret && !revealed ? 'password' : 'text'"
          :placeholder="field.placeholder"
          :disabled="disabled"
          :aria-invalid="error ? 'true' : undefined"
          :aria-describedby="describedBy"
          autocomplete="off"
          autocapitalize="off"
          autocorrect="off"
          spellcheck="false"
          class="h-full min-w-0 flex-1 bg-transparent px-3 font-mono text-sm text-ink outline-none disabled:opacity-60"
          @blur="emit('blur')"
          @keydown.enter.prevent="emit('enter')"
        />
        <button
          v-if="field.secret"
          type="button"
          class="mr-1 flex size-8 items-center justify-center rounded-md text-ink-muted hover:bg-raised hover:text-ink"
          :aria-label="revealed ? `Hide ${field.label}` : `Show ${field.label}`"
          :aria-pressed="revealed"
          @click="revealed = !revealed"
        >
          <AppIcon :name="revealed ? 'eye-off' : 'eye'" />
        </button>
      </div>
      <button
        type="button"
        class="flex shrink-0 items-center gap-1.5 rounded-lg border border-line-strong bg-raised px-3 text-sm text-ink hover:bg-selected disabled:opacity-50"
        :disabled="disabled"
        @click="paste"
      >
        <AppIcon name="paste" />
        Paste
      </button>
    </div>
    <p v-if="field.help" :id="helpId" class="text-xs text-ink-dim">{{ field.help }}</p>
    <p v-if="pasteNote" class="text-xs text-ink-muted">{{ pasteNote }}</p>
    <p v-if="error" :id="errorId" role="alert" class="flex items-start gap-1.5 text-xs text-fail" data-testid="field-error">
      <AppIcon name="alert" size="sm" />
      <span>{{ error }}</span>
    </p>
  </div>
</template>
