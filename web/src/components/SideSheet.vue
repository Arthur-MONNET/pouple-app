<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue';
import IconButton from './IconButton.vue';

defineProps<{ title: string }>();
const emit = defineEmits<{ close: [] }>();

const panel = ref<HTMLElement | null>(null);
let previous: Element | null = null;

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/** Keeps Tab inside the sheet while it is open. */
function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    emit('close');
    return;
  }
  if (event.key !== 'Tab' || !panel.value) return;
  const items = [...panel.value.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((el) => el.offsetParent !== null);
  const first = items[0];
  const last = items.at(-1);
  if (!first || !last) return;
  if (event.shiftKey && document.activeElement === first) (event.preventDefault(), last.focus());
  else if (!event.shiftKey && document.activeElement === last) (event.preventDefault(), first.focus());
}

onMounted(async () => {
  previous = document.activeElement;
  await nextTick();
  panel.value?.focus();
});
onBeforeUnmount(() => (previous instanceof HTMLElement ? previous.focus() : undefined));
</script>

<template>
  <Teleport to="body">
    <div class="fixed inset-0 z-40 flex justify-end" @keydown="onKeydown">
      <div class="absolute inset-0 bg-black/60" aria-hidden="true" @click="emit('close')" />
      <div
        ref="panel"
        role="dialog"
        aria-modal="true"
        :aria-label="title"
        tabindex="-1"
        class="relative flex h-full w-full max-w-3xl flex-col overflow-y-auto border-l border-line-strong bg-canvas shadow-2xl outline-none"
      >
        <div class="flex items-center justify-between border-b border-line-soft px-5 py-3">
          <h2 class="text-base font-semibold text-ink">{{ title }}</h2>
          <IconButton label="Close" icon="close" @click="emit('close')" />
        </div>
        <div class="flex-1 p-5"><slot /></div>
      </div>
    </div>
  </Teleport>
</template>
