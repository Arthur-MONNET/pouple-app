<script setup lang="ts">
import { computed, ref } from 'vue';
import { useShortcuts } from '../composables/useShortcuts';
import { t } from '../i18n';
import { chordOf, displayShortcut, isMac, type ShortcutId } from '../lib/shortcuts';

const props = defineProps<{ id: ShortcutId; label: string }>();
const { shortcuts, set, reset } = useShortcuts();

/** The field listens for the next combination only while it is armed; leaving it, or Escape, disarms it unchanged. */
const listening = ref(false);
const problem = ref<'unusable' | 'taken' | null>(null);
const shown = computed(() => displayShortcut(shortcuts.value[props.id], isMac()));

function onKeydown(event: KeyboardEvent): void {
  if (!listening.value) return;
  if (event.key === 'Escape') {
    listening.value = false;
    problem.value = null;
    return;
  }
  const chord = chordOf(event);
  // A modifier on its own is the start of a combination, not the end.
  if (chord === null) return;
  event.preventDefault();
  problem.value = set(props.id, chord);
  if (problem.value === null) listening.value = false;
}

function onReset(): void {
  problem.value = reset(props.id);
  listening.value = false;
}
</script>

<template>
  <div class="flex flex-col gap-1.5 min-[560px]:items-end">
    <div class="flex items-center gap-2">
      <button
        type="button"
        class="inline-flex h-10 min-w-28 items-center justify-center rounded-lg border bg-canvas px-3 text-sm text-ink outline-none transition focus:border-accent motion-reduce:transition-none"
        :class="listening ? 'border-accent' : 'border-line-strong hover:border-accent'"
        :aria-label="$t('shortcuts.change', { name: label, shortcut: shown })"
        :data-testid="`shortcut-${id}`"
        @click="(listening = true), (problem = null)"
        @keydown="onKeydown"
        @blur="listening = false"
      >
        {{ listening ? $t('shortcuts.press') : shown }}
      </button>
      <button type="button" class="h-10 rounded-lg px-2.5 text-sm text-ink-muted transition hover:bg-raised hover:text-ink motion-reduce:transition-none" :data-testid="`shortcut-reset-${id}`" @click="onReset">
        {{ $t('shortcuts.reset') }}
      </button>
    </div>
    <p v-if="problem" class="text-xs text-fail" role="alert">{{ t(`shortcuts.${problem}`) }}</p>
  </div>
</template>
