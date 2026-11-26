<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue';
import { useAssistantStore } from '../composables/assistantStore';

/** How long the undo is offered. */
const UNDO_MS = 5000;

const store = useAssistantStore();
const paused = ref(false);
let timer: ReturnType<typeof setTimeout> | undefined;

function arm(): void {
  clearTimeout(timer);
  if (store.lastArrange.value && !paused.value) timer = setTimeout(store.dismissArrange, UNDO_MS);
}

// A new arrangement restarts the countdown; pointing at the toast or focusing it holds it, so it never vanishes under the hand.
watch(() => store.lastArrange.value?.seq, arm);
watch(paused, arm);
onBeforeUnmount(() => clearTimeout(timer));
</script>

<template>
  <div class="pointer-events-none absolute inset-x-0 bottom-4 z-40 flex justify-center px-4" role="status" aria-live="polite">
    <div
      v-if="store.lastArrange.value"
      class="pointer-events-auto flex items-center gap-3 rounded-lg border border-line-strong bg-raised px-3.5 py-2 text-sm text-ink shadow-xl shadow-black/40"
      data-testid="undo-toast"
      @pointerenter="paused = true"
      @pointerleave="paused = false"
      @focusin="paused = true"
      @focusout="paused = false"
    >
      <span>{{ store.lastArrange.value.message }}</span>
      <button
        type="button"
        class="rounded-md px-2 py-1 text-sm font-medium text-accent transition hover:bg-selected focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent motion-reduce:transition-none"
        data-testid="undo-button"
        @click="store.undoArrange()"
      >
        {{ $t('shelf.undo') }}
      </button>
    </div>
  </div>
</template>
