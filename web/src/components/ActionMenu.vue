<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, useId, watch } from 'vue';
import AppIcon from './AppIcon.vue';

export interface MenuItem {
  id: string;
  label: string;
  /** Destroys something: shown apart, in the failure colour. */
  danger?: boolean;
}

const props = withDefaults(defineProps<{ label: string; items: MenuItem[]; disabled?: boolean }>(), { disabled: false });
const emit = defineEmits<{ select: [id: string] }>();

const open = ref(false);
const menuId = useId();
const trigger = ref<HTMLButtonElement | null>(null);
const menu = ref<HTMLElement | null>(null);

const entries = (): HTMLButtonElement[] => [...(menu.value?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') ?? [])];

async function show(focus: 'first' | 'last' = 'first'): Promise<void> {
  open.value = true;
  await nextTick();
  const items = entries();
  (focus === 'first' ? items[0] : items.at(-1))?.focus();
}

function close(returnFocus: boolean): void {
  open.value = false;
  if (returnFocus) trigger.value?.focus();
}

function pick(id: string): void {
  close(true);
  emit('select', id);
}

/** Arrows move between the entries, Home and End jump, Escape and Tab close: the keys of a menu. */
function onKey(event: KeyboardEvent): void {
  const items = entries();
  const index = items.indexOf(document.activeElement as HTMLButtonElement);
  const move: Record<string, number> = { ArrowDown: index + 1, ArrowUp: index - 1, Home: 0, End: items.length - 1 };
  if (event.key in move) {
    event.preventDefault();
    const next = ((move[event.key] ?? 0) + items.length) % items.length;
    items[next]?.focus();
  } else if (event.key === 'Escape') {
    event.preventDefault();
    close(true);
  } else if (event.key === 'Tab') {
    close(false);
  }
}

function onOutside(event: PointerEvent): void {
  const target = event.target as Node | null;
  if (target && !menu.value?.contains(target) && !trigger.value?.contains(target)) close(false);
}

watch(open, (isOpen) => {
  if (isOpen) document.addEventListener('pointerdown', onOutside);
  else document.removeEventListener('pointerdown', onOutside);
});
onBeforeUnmount(() => document.removeEventListener('pointerdown', onOutside));
</script>

<template>
  <div class="relative">
    <button
      ref="trigger"
      type="button"
      class="inline-flex size-8 items-center justify-center rounded-md text-ink-muted transition hover:bg-raised hover:text-ink disabled:cursor-not-allowed disabled:opacity-50 motion-reduce:transition-none"
      :aria-label="props.label"
      :title="props.label"
      aria-haspopup="menu"
      :aria-expanded="open"
      :aria-controls="menuId"
      :disabled="disabled"
      @click="open ? close(false) : show()"
      @keydown.down.prevent="show('first')"
      @keydown.up.prevent="show('last')"
    >
      <AppIcon name="more" />
    </button>
    <ul
      v-if="open"
      :id="menuId"
      ref="menu"
      role="menu"
      :aria-label="props.label"
      class="absolute right-0 top-full z-30 mt-1 flex min-w-44 flex-col rounded-lg border border-line-strong bg-raised p-1 shadow-xl shadow-black/40"
      @keydown="onKey"
    >
      <li v-for="(item, index) in items" :key="item.id" role="none" :class="{ 'mt-1 border-t border-line pt-1': item.danger && index > 0 }">
        <button
          type="button"
          role="menuitem"
          tabindex="-1"
          class="flex w-full items-center rounded-md px-2.5 py-1.5 text-left text-sm transition focus:outline-none motion-reduce:transition-none"
          :class="{ 'text-fail hover:bg-fail/10 focus:bg-fail/10': item.danger, 'text-ink hover:bg-selected focus:bg-selected': !item.danger }"
          @click="pick(item.id)"
        >
          {{ item.label }}
        </button>
      </li>
    </ul>
  </div>
</template>
